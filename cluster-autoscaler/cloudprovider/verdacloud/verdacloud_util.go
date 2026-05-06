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
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// asgSpecNameRe enforces a DNS-label-ish format for ASG names and hostname prefixes.
var asgSpecNameRe = regexp.MustCompile(`^[a-z0-9A-Z]+[a-z0-9A-Z\-\.\_]*[a-z0-9A-Z]+$|^[a-z0-9A-Z]{1}$`)

func convertConfigLabelsToK8sLabels(labels []string, asg *Asg) string {
	if asg == nil {
		return ""
	}
	result := make([]string, 0, len(labels)+2)
	result = append(result, labels...)
	result = append(result, fmt.Sprintf("%s=%s", NodeGroupLabelKey, asg.Name))
	if isGPUInstanceType(asg.instanceType) {
		result = append(result, fmt.Sprintf("%s=%s", AcceleratorLabel, asg.instanceType))
	}
	return strings.Join(result, ",")
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
