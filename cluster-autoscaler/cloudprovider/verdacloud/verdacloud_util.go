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
	"slices"
	"strconv"
	"strings"
	"text/template"
	"text/template/parse"

	apiv1 "k8s.io/api/core/v1"
)

// asgSpecNameRe: lowercase DNS label, so generated hostnames are stored and matched verbatim.
var asgSpecNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// validateHostnameBase rejects names whose generated hostname would not parse back to the same group.
func validateHostnameBase(kind, name string) error {
	if !asgSpecNameRe.MatchString(name) {
		return fmt.Errorf("invalid %s %q: must be lowercase letters, digits and '-', starting and ending with a letter or digit", kind, name)
	}
	if strings.Contains(name, ASG_SEPARATOR) || strings.HasSuffix(name, "-"+ASG_SEPARATOR_MAGIC_NUMBER) {
		return fmt.Errorf("invalid %s %q: must not contain %q or end with %q (reserved hostname separator)",
			kind, name, ASG_SEPARATOR, "-"+ASG_SEPARATOR_MAGIC_NUMBER)
	}
	return nil
}

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
	if err := validateHostnameBase("ASG name", asgName); err != nil {
		return nil, err
	}

	hostnamePrefix := ""
	if len(parts) == 5 && parts[4] != "" {
		hostnamePrefix = parts[4]
		if err := validateHostnameBase("hostname prefix", hostnamePrefix); err != nil {
			return nil, err
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

// formatProviderID returns "verdacloud://<lowercase-location>/<hostname>".
// Location is lowercased to stay byte-equal with what verdacloud-CCM writes
// onto Node.Spec.ProviderID (CCM normalizes location to lowercase).
func formatProviderID(location, hostname string) string {
	return verdacloudProviderIDPrefix + strings.ToLower(location) + "/" + hostname
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

// locationFromProviderID returns <location> from "verdacloud://<location>/<hostname>", or "" if absent.
func locationFromProviderID(providerID string) string {
	rest, ok := strings.CutPrefix(providerID, verdacloudProviderIDPrefix)
	if !ok {
		return ""
	}
	location, _, found := strings.Cut(rest, "/")
	if !found {
		return ""
	}
	return location
}

// kubeletNodeLabelsFlag renders labels in kubelet --node-labels format, sorted for stable output.
func kubeletNodeLabelsFlag(labels map[string]string) string {
	pairs := make([]string, 0, len(labels))
	for k, v := range labels {
		pairs = append(pairs, k+"="+v)
	}
	slices.Sort(pairs)
	return strings.Join(pairs, ",")
}

// kubeletTaintsFlag renders taints in kubelet --register-with-taints format, sorted for stable output.
func kubeletTaintsFlag(taints []apiv1.Taint) string {
	specs := make([]string, 0, len(taints))
	for _, t := range taints {
		spec := t.Key
		if t.Value != "" {
			spec += "=" + t.Value
		}
		specs = append(specs, spec+":"+string(t.Effect))
	}
	slices.Sort(specs)
	return strings.Join(specs, ",")
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

// StartupScriptTemplateData is the only text/template root for startupScript; extending it is a code change, not operator config.
// ProviderID and location labels arrive from CCM after join; unknown template keys fail scale-up via missingkey=error.
type StartupScriptTemplateData struct {
	// Filled from process env on the autoscaler Pod (typically envFrom Secret), not cloud-config.
	MasterIP     string
	MasterPort   string
	JoinToken    string
	JoinHashFull string

	// Per-group kubelet flag values (--node-labels / --register-with-taints); must reach kubelet
	// so real nodes match the template node the autoscaler simulated.
	NodeLabels string
	NodeTaints string
}

// renderStartupScript expands operator startupScript templates into bytes for Verda startup-script creation.
// Template parse errors and missing/unset keys fail during scale-up before any VM exists (missingkey=error).
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

// referencedTemplateFields returns the set of top-level field names
// referenced via {{.FieldName}} in the operator's startupScript template.
// Used at startup to catch the empty-Secret-value case that missingkey=error
// can't (the field is present on the struct but its value is empty).
func referencedTemplateFields(operatorBody []byte) (map[string]bool, error) {
	tmpl, err := template.New("startupScript").
		Option("missingkey=error").
		Parse(string(operatorBody))
	if err != nil {
		return nil, fmt.Errorf("parse startupScript template: %w", err)
	}
	fields := make(map[string]bool)
	if tmpl.Tree != nil {
		visitTemplateNode(tmpl.Tree.Root, fields)
	}
	return fields, nil
}

// visitTemplateNode recursively collects top-level field names from a parse.Node.
func visitTemplateNode(node parse.Node, fields map[string]bool) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *parse.ListNode:
		if n == nil {
			return
		}
		for _, child := range n.Nodes {
			visitTemplateNode(child, fields)
		}
	case *parse.ActionNode:
		visitTemplatePipe(n.Pipe, fields)
	case *parse.IfNode:
		visitTemplatePipe(n.Pipe, fields)
		visitTemplateNode(n.List, fields)
		visitTemplateNode(n.ElseList, fields)
	case *parse.RangeNode:
		visitTemplatePipe(n.Pipe, fields)
		visitTemplateNode(n.List, fields)
		visitTemplateNode(n.ElseList, fields)
	case *parse.WithNode:
		visitTemplatePipe(n.Pipe, fields)
		visitTemplateNode(n.List, fields)
		visitTemplateNode(n.ElseList, fields)
	}
}

// visitTemplatePipe extracts top-level field names from a pipe's commands.
// {{.MasterIP}} contributes "MasterIP"; {{.X.Y}} contributes "X".
func visitTemplatePipe(pipe *parse.PipeNode, fields map[string]bool) {
	if pipe == nil {
		return
	}
	for _, cmd := range pipe.Cmds {
		for _, arg := range cmd.Args {
			if field, ok := arg.(*parse.FieldNode); ok {
				if len(field.Ident) > 0 {
					fields[field.Ident[0]] = true
				}
			}
		}
	}
}
