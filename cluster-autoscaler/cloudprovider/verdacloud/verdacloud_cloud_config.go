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
	"strings"

	apiv1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
)

const (
	minOSVolumeSizeGB   = 50
	BillingContractSpot = "SPOT"
)

type cloudConfig struct {
	Image              imageConfig            `json:"image"`
	SSHKeyIDs          []string               `json:"sshKeyIDs"`
	BillingConfig      billingConfig          `json:"billingConfig"`
	Labels             []string               `json:"labels"`
	Debug              bool                   `json:"debug"`
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
	// Per-VM identity (provider-id, labels) is NOT in this set — the
	// verdacloud-cloud-controller-manager sets those post-join.
	MasterIP     string `json:"-"`
	MasterPort   string `json:"-"`
	JoinToken    string `json:"-"`
	JoinHashFull string `json:"-"`

	// ReapOrphanNodes opts into deleting K8s Nodes whose VerdaCloud VMs are
	// gone. Default false; intended only for clusters without a CCM.
	ReapOrphanNodes bool `json:"reapOrphanNodes"`

	// ReapOrphanNodesAfterCycles is the consecutive-Refresh-cycles threshold
	// before a missing hostname's Node is deleted. Default 3 when 0.
	ReapOrphanNodesAfterCycles int `json:"reapOrphanNodesAfterCycles"`
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
}

type additionalVolume struct {
	Name string
	Size int
	Type string
}

func (cfg *cloudConfig) validate() error {
	var errs []error

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

func (cfg *cloudConfig) isValid() bool {
	err := cfg.validate()
	if err != nil {
		klog.Error(err)
		return false
	}
	return true
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
