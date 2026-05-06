/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package verdacloud

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"text/template"
)

// asgSpecNameRe enforces a DNS-label-ish format for ASG names and hostname prefixes.
var asgSpecNameRe = regexp.MustCompile(`^[a-z0-9A-Z]+[a-z0-9A-Z\-\.\_]*[a-z0-9A-Z]+$|^[a-z0-9A-Z]{1}$`)

func parseAsgSpec(spec string) (*VerdacloudAsgSpec, error) {
	parts := strings.Split(spec, ":")
	if len(parts) != 4 && len(parts) != 5 {
		return nil, fmt.Errorf("invalid ASG spec (expected min:max:instance-type:asg-name[:hostname-prefix]): %s", spec)
	}

	minSize, err := strconv.Atoi(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid min size: %s", parts[0])
	}
	maxSize, err := strconv.Atoi(parts[1])
	if err != nil {
		return nil, fmt.Errorf("invalid max size: %s", parts[1])
	}

	asgName := parts[3]
	if !asgSpecNameRe.MatchString(asgName) {
		return nil, fmt.Errorf("invalid ASG name: %s", asgName)
	}

	hostnamePrefix := ""
	if len(parts) == 5 && parts[4] != "" {
		hostnamePrefix = parts[4]
		if !asgSpecNameRe.MatchString(hostnamePrefix) {
			return nil, fmt.Errorf("invalid hostname prefix: %s", hostnamePrefix)
		}
	}

	if minSize < 0 || maxSize < 0 {
		return nil, fmt.Errorf("min/max sizes must be non-negative: min=%d, max=%d", minSize, maxSize)
	}
	if minSize > maxSize {
		return nil, fmt.Errorf("min size %d cannot exceed max size %d", minSize, maxSize)
	}

	return &VerdacloudAsgSpec{
		minSize:        minSize,
		maxSize:        maxSize,
		instanceType:   parts[2],
		name:           asgName,
		hostnamePrefix: hostnamePrefix,
	}, nil
}

func isGPUInstanceType(instanceType string) bool {
	return !strings.HasPrefix(strings.ToUpper(instanceType), "CPU.")
}

func instanceRefFromProviderId(providerId string) (*InstanceRef, error) {
	if !strings.HasPrefix(providerId, verdacloudProviderIDPrefix) {
		return nil, fmt.Errorf("not a VerdaCloud provider ID: %s", providerId)
	}
	providerIdBase := strings.TrimPrefix(providerId, verdacloudProviderIDPrefix)
	parts := strings.Split(providerIdBase, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid provider ID: %s", providerId)
	}
	return &InstanceRef{Hostname: parts[len(parts)-1], ProviderID: providerId}, nil
}

func extractAsgNameFromHostname(hostname string) (string, error) {
	separator := fmt.Sprintf("-%s-", ASG_SEPARATOR_MAGIC_NUMBER)

	parts := strings.Split(hostname, separator)
	if len(parts) == 2 {
		asgName := parts[0]
		if asgName == "" {
			return "", fmt.Errorf("empty ASG name extracted from hostname: %s", hostname)
		}
		return asgName, nil
	}

	return "", fmt.Errorf("hostname does not contain magic separator '%s': %s", separator, hostname)
}

// StartupScriptTemplateData defines the *complete* set of variables the
// operator's startupScript may reference via Go text/template syntax.
//
// Adding a field here is a deliberate Go-code change reviewed in PR.
// Operators cannot extend this set — the script body must work with what
// is exposed here, or template execution fails fast (we set
// Option("missingkey=error") on the template).
//
// Per-VM identity (provider-id, labels) is intentionally NOT in this
// struct. verdacloud-cloud-controller-manager patches spec.providerID and
// the standard topology / instance-type labels onto each Node post-join;
// the script does not need to know its providerID in advance because
// kubelet starts with --cloud-provider=external.
//
// Pattern reference: this mirrors equinixmetal/manager_rest.go's
// CloudInitTemplateData (BootstrapTokenID / BootstrapTokenSecret /
// APIServerEndpoint / NodeGroup) and cherryservers' equivalent.
type StartupScriptTemplateData struct {
	// Cluster-wide; sourced from the cluster-autoscaler-startup-env Secret
	// via envFrom on the autoscaler container.
	MasterIP     string
	MasterPort   string
	JoinToken    string
	JoinHashFull string
}

// renderStartupScript executes the operator's startupScript template against
// the cluster-wide values and returns the bytes to send to Verda's
// CreateStartupScript API.
//
// Failure modes (caught at scale-up time, before any VM is created):
//   - parse error: operator's template has invalid Go-template syntax.
//   - missing-key error: operator referenced {{.NotInStruct}} that doesn't
//     exist on StartupScriptTemplateData. Typo detection.
//   - execute error: anything else surfaced by template.Execute.
func renderStartupScript(operatorBody []byte, vars StartupScriptTemplateData) ([]byte, error) {
	tmpl, err := template.New("startupScript").
		Option("missingkey=error").
		Parse(string(operatorBody))
	if err != nil {
		return nil, fmt.Errorf("parse startupScript template: %w", err)
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, vars); err != nil {
		return nil, fmt.Errorf("execute startupScript template: %w", err)
	}
	return out.Bytes(), nil
}
