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
	"errors"
	"fmt"
	"regexp"
	"strings"

	apiv1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
	kubeletapis "k8s.io/kubelet/pkg/apis"
)

const (
	minOSVolumeSizeGB = 50
	// BillingContractSpot is the billing contract value selecting spot instances.
	BillingContractSpot = "SPOT"
)

// clusterNameRe keeps clusterName a valid Kubernetes label value (it is published as ClusterLabelKey).
var clusterNameRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,61}[A-Za-z0-9])?$`)

type cloudConfig struct {
	// ClusterName is optional and only labels nodes (ClusterLabelKey). Group membership comes from the
	// hostname prefix alone, which Verda does not allow to change after deployment.
	ClusterName        string                 `json:"clusterName"`
	Image              imageConfig            `json:"image"`
	SSHKeyIDs          []string               `json:"sshKeyIDs"`
	BillingConfig      billingConfig          `json:"billingConfig"`
	Labels             []string               `json:"labels"`
	AvailableLocations []string               `json:"availableLocations"`
	StartupScript      string                 `json:"startupScript"`
	Taints             []apiv1.Taint          `json:"taints"`
	Groups             map[string]GroupConfig `json:"groups"`
	OSVolumeSize       int                    `json:"osVolumeSize"`

	// Cluster-wide values fed into the operator's startupScript template
	// at scale-up time. Populated from env vars on the autoscaler container
	// (sourced from the cluster-autoscaler-startup-env Secret via envFrom),
	// not from cluster-config.json. `json:"-"` keeps them out of the schema.
	//
	// Per-VM identity (provider-id, location labels) is NOT in this set — the
	// verdacloud-cloud-controller-manager sets those post-join.
	MasterIP     string `json:"-"`
	MasterPort   string `json:"-"`
	JoinToken    string `json:"-"`
	JoinHashFull string `json:"-"`
}

// GroupConfig overrides global config per-ASG.
type GroupConfig struct {
	BillingConfig      *billingConfig     `json:"billingConfig,omitempty"`
	Labels             []string           `json:"labels,omitempty"`
	Taints             []apiv1.Taint      `json:"taints,omitempty"`
	OSVolumeSize       *int               `json:"osVolumeSize,omitempty"`
	AvailableLocations []string           `json:"availableLocations,omitempty"`
	AdditionalVolumes  []additionalVolume `json:"additionalVolumes,omitempty"`
}

type imageConfig struct {
	GPU string `json:"gpu"`
	CPU string `json:"cpu"`
}

type billingConfig struct {
	Price    string `json:"price"`
	Contract string `json:"contract"`
}

type nodeConfig struct {
	IsSpot             bool
	Image              string
	StartupScript      string
	SSHKeyIDs          []string
	OSVolumeSize       int
	Labels             []string
	Volumes            []additionalVolume
	Taints             []apiv1.Taint
	Contract           string
	Price              string
	AvailableLocations []string
	ClusterName        string
}

type additionalVolume struct {
	Name string
	Size int
	Type string
}

func (cfg *cloudConfig) validate() error {
	var errs []error

	if cfg.ClusterName != "" && !clusterNameRe.MatchString(cfg.ClusterName) {
		errs = append(errs, fmt.Errorf("clusterName %q must be 1-63 characters of letters, digits, '.', '_' or '-', starting and ending with a letter or digit", cfg.ClusterName))
	}

	errs = append(errs, validateKubeletLabels("labels", cfg.Labels)...)
	for name, group := range cfg.Groups {
		errs = append(errs, validateKubeletLabels(fmt.Sprintf("groups.%s.labels", name), group.Labels)...)
	}

	if cfg.Image.GPU == "" && cfg.Image.CPU == "" {
		errs = append(errs, fmt.Errorf("at least one image (GPU or CPU) must be specified"))
	}

	if len(cfg.SSHKeyIDs) == 0 {
		errs = append(errs, fmt.Errorf("sshKeyIDs is required (at least one SSH key must be provided)"))
	}

	if cfg.StartupScript == "" {
		errs = append(errs, fmt.Errorf("startupScript is required (must be base64 encoded)"))
	}

	if len(cfg.AvailableLocations) == 0 {
		errs = append(errs, fmt.Errorf("availableLocations is required (at least one location must be specified)"))
	}

	if len(errs) > 0 {
		return fmt.Errorf("invalid cloud configuration: %w", errors.Join(errs...))
	}

	return nil
}

// validateKubeletLabels rejects labels kubelet refuses in --node-labels (it exits at startup, so the VM never joins).
func validateKubeletLabels(field string, labels []string) []error {
	var errs []error
	for _, label := range labels {
		key, _, ok := strings.Cut(label, "=")
		if !ok {
			continue // mergeLabels drops malformed entries with a warning
		}
		namespace, _, hasNamespace := strings.Cut(key, "/")
		if !hasNamespace {
			continue
		}
		restricted := namespace == "kubernetes.io" || strings.HasSuffix(namespace, ".kubernetes.io") ||
			namespace == "k8s.io" || strings.HasSuffix(namespace, ".k8s.io")
		if restricted && !kubeletapis.IsKubeletLabel(key) {
			errs = append(errs, fmt.Errorf("%s: label %q is in a namespace kubelet may not self-assign; use a non-Kubernetes prefix such as verda.com/ or example.com/", field, key))
		}
	}
	return errs
}

// GetNodeConfig merges global + group overrides; group wins on conflict.
func (cfg *cloudConfig) GetNodeConfig(asgName string) *nodeConfig {
	billingCfg := cfg.BillingConfig

	labels := mergeLabels(cfg.Labels, nil)
	taints := mergeTaints(cfg.Taints, nil)

	osVolumeSize := max(cfg.OSVolumeSize, minOSVolumeSizeGB)

	availableLocations := cfg.AvailableLocations
	var volumes []additionalVolume

	if groupCfg, ok := cfg.Groups[asgName]; ok {
		if groupCfg.BillingConfig != nil {
			billingCfg = *groupCfg.BillingConfig
		}
		if len(groupCfg.Labels) > 0 {
			labels = mergeLabels(cfg.Labels, groupCfg.Labels)
		}
		if len(groupCfg.Taints) > 0 {
			taints = mergeTaints(cfg.Taints, groupCfg.Taints)
		}
		if groupCfg.OSVolumeSize != nil && *groupCfg.OSVolumeSize >= minOSVolumeSizeGB {
			osVolumeSize = *groupCfg.OSVolumeSize
		}
		if len(groupCfg.AvailableLocations) > 0 {
			availableLocations = groupCfg.AvailableLocations
		}
		if len(groupCfg.AdditionalVolumes) > 0 {
			volumes = make([]additionalVolume, len(groupCfg.AdditionalVolumes))
			copy(volumes, groupCfg.AdditionalVolumes)
		}
	}

	nc := &nodeConfig{
		ClusterName:        cfg.ClusterName,
		IsSpot:             billingCfg.Contract == BillingContractSpot,
		Image:              "", // set by caller based on GPU/CPU
		StartupScript:      cfg.StartupScript,
		SSHKeyIDs:          cfg.SSHKeyIDs,
		OSVolumeSize:       osVolumeSize,
		Labels:             labels,
		Volumes:            volumes,
		Taints:             make([]apiv1.Taint, len(taints)),
		Contract:           billingCfg.Contract,
		Price:              billingCfg.Price,
		AvailableLocations: availableLocations,
	}
	copy(nc.Taints, taints)

	return nc
}

// mergeLabels merges labels; group wins on key conflict.
func mergeLabels(global, group []string) []string {
	labelMap := make(map[string]string)
	addAll := func(labels []string, source string) {
		for _, label := range labels {
			parts := strings.SplitN(label, "=", 2)
			if len(parts) != 2 {
				klog.Warningf("dropping malformed %s label %q: expected key=value", source, label)
				continue
			}
			labelMap[parts[0]] = parts[1]
		}
	}
	addAll(global, "global")
	addAll(group, "group")
	result := make([]string, 0, len(labelMap))
	for k, v := range labelMap {
		result = append(result, k+"="+v)
	}
	return result
}

// mergeTaints merges taints; group wins on same Key.
func mergeTaints(global, group []apiv1.Taint) []apiv1.Taint {
	taintMap := make(map[string]apiv1.Taint)
	for _, taint := range global {
		taintMap[taint.Key] = taint
	}
	for _, taint := range group {
		taintMap[taint.Key] = taint
	}
	result := make([]apiv1.Taint, 0, len(taintMap))
	for _, taint := range taintMap {
		result = append(result, taint)
	}
	return result
}
