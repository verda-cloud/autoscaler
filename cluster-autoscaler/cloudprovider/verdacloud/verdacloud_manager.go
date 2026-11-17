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
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider"
	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider/verdacloud/verda-go"
	klog "k8s.io/klog/v2"
)

const (
	verdacloudProviderIDPrefix = "verdacloud://"
	// LabelLocation matches the CCM's verda.com/location label (lowercase location).
	LabelLocation          = "verda.com/location"
	defaultPodAmountsLimit = 110
	refreshInterval        = 1 * time.Minute // throttles API calls during rapid loops
)

// VerdacloudManager manages Verdacloud resources for the cluster autoscaler.
type VerdacloudManager struct {
	cfg         *cloudConfig
	sdkProvider *verdacloudSDKProvider
	dcService   dcService
	asgs        *autoScalingGroups

	// Unix-nano timestamp of the last successful regenerate.
	// Atomic keeps concurrent Refresh calls race-free.
	lastRefreshNanos atomic.Int64
}

type asgTemplate struct {
	InstanceType *InstanceResource
	Tags         map[string]string
}

// InstanceResource represents the resource configuration of a Verdacloud instance type.
type InstanceResource struct {
	InstanceType string
	Arch         string
	CPU          int64
	Memory       int64
	GPU          int64
}

func createVerdacloudManager(cloudReader io.Reader, discoveryOpts cloudprovider.NodeGroupDiscoveryOptions) (*VerdacloudManager, error) {
	cfg := &cloudConfig{}
	if cloudReader != nil {
		decoder := json.NewDecoder(cloudReader)
		if err := decoder.Decode(cfg); err != nil {
			return nil, err
		}
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	cfg = verifyCloudConfigAndPatch(cfg)

	// Join/bootstrap template vars (MASTER_IP, MASTER_PORT, JOIN_TOKEN, JOIN_HASH_FULL) load from autoscaler Pod env only, not cloud-config.
	cfg.MasterIP = os.Getenv("MASTER_IP")
	cfg.MasterPort = os.Getenv("MASTER_PORT")
	cfg.JoinToken = os.Getenv("JOIN_TOKEN")
	cfg.JoinHashFull = os.Getenv("JOIN_HASH_FULL")

	// Fail fast if the operator's startupScript references a {{.X}} whose
	// corresponding env-var-supplied value is empty (missingkey=error
	// catches absent fields, not present-but-empty values).
	if err := validateStartupTemplateValues(cfg); err != nil {
		return nil, err
	}

	sdkProvider, err := createVerdacloudSDKProvider()
	if err != nil {
		return nil, err
	}

	dcService := newVerdacloudWrapper(sdkProvider.client)

	manager := &VerdacloudManager{
		cfg:         cfg,
		sdkProvider: sdkProvider,
		dcService:   dcService,
		asgs:        nil,
	}

	manager.asgs, err = newAutoScalingGroups(dcService, discoveryOpts.NodeGroupSpecs, cfg)
	if err != nil {
		return nil, err
	}

	return manager, nil
}

// Refresh refreshes the state of the manager from the cloud provider.
func (m *VerdacloudManager) Refresh() error {
	last := time.Unix(0, m.lastRefreshNanos.Load())
	if last.Add(refreshInterval).After(time.Now()) {
		return nil
	}
	return m.forceRefresh()
}

func (m *VerdacloudManager) forceRefresh() error {
	if err := m.asgs.regenerate(); err != nil {
		return err
	}
	m.lastRefreshNanos.Store(time.Now().UnixNano())
	return nil
}

// ScaleUpAsg scales up the ASG by the given delta.
func (m *VerdacloudManager) ScaleUpAsg(asg *Asg, delta int) error {
	return m.asgs.scaleUpAsg(asg, delta)
}

func (m *VerdacloudManager) getAsgs() []*Asg {
	return m.asgs.getAsgs()
}

// GetAsgByRef returns the ASG for the given reference.
func (m *VerdacloudManager) GetAsgByRef(ref AsgRef) (*Asg, error) {
	return m.asgs.GetAsgByRef(ref)
}

// GetAsgForInstance returns the ASG that the given instance belongs to.
func (m *VerdacloudManager) GetAsgForInstance(ref *InstanceRef) (*Asg, error) {
	if ref == nil {
		return nil, errors.New("ref is required")
	}

	return m.asgs.FindASGForInstance(ref)
}

// DeleteInstances deletes the given instances in parallel.
func (m *VerdacloudManager) DeleteInstances(instanceRefs []InstanceRef) error {
	if len(instanceRefs) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	errsCh := make(chan error, len(instanceRefs))
	sem := make(chan struct{}, MAX_CONCURRENT_INSTANCE_CREATIONS)
	for _, ref := range instanceRefs {
		wg.Add(1)
		sem <- struct{}{}
		go func(r InstanceRef) {
			defer func() { <-sem; wg.Done() }()
			if err := m.asgs.DeleteInstance(r); err != nil {
				errsCh <- err
			}
		}(ref)
	}
	wg.Wait()
	close(errsCh)

	var errs []error
	for err := range errsCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to delete %d/%d instances: %w", len(errs), len(instanceRefs), errors.Join(errs...))
	}
	return nil
}

// DeleteAsg deletes the given ASG.
func (m *VerdacloudManager) DeleteAsg(asg *Asg) error {
	return m.asgs.DeleteAsg(asg.AsgRef)
}

func verifyCloudConfigAndPatch(cfg *cloudConfig) *cloudConfig {

	if cfg.BillingConfig.Contract == "" {
		cfg.BillingConfig.Contract = "PAY_AS_YOU_GO"
	}
	if cfg.BillingConfig.Price == "" {
		cfg.BillingConfig.Price = "FIXED_PRICE"
	}
	return cfg
}

// validateStartupTemplateValues errors if any template field referenced by
// cfg.StartupScript has an empty value on cfg. Closes the empty-Secret-value
// gap that missingkey=error doesn't detect.
func validateStartupTemplateValues(cfg *cloudConfig) error {
	if cfg.StartupScript == "" {
		// cfg.isValid() already requires startupScript; defensive check only.
		return nil
	}
	decoded, err := base64.StdEncoding.DecodeString(cfg.StartupScript)
	if err != nil {
		return fmt.Errorf("base64-decode startupScript: %w", err)
	}
	refs, err := referencedTemplateFields(decoded)
	if err != nil {
		return err
	}

	type fieldCheck struct {
		name, envVar, value string
	}
	checks := []fieldCheck{
		{"MasterIP", "MASTER_IP", cfg.MasterIP},
		{"MasterPort", "MASTER_PORT", cfg.MasterPort},
		{"JoinToken", "JOIN_TOKEN", cfg.JoinToken},
		{"JoinHashFull", "JOIN_HASH_FULL", cfg.JoinHashFull},
	}
	var missing []string
	for _, c := range checks {
		if refs[c.name] && c.value == "" {
			missing = append(missing, c.envVar)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"startupScript references template variables but the corresponding env vars are empty: %s. "+
				"Populate them in the cluster-autoscaler-startup-env Secret and rolling-restart the autoscaler",
			strings.Join(missing, ", "),
		)
	}
	return nil
}

// GetAvailableMachineTypes returns a list of available machine types.
func (m *VerdacloudManager) GetAvailableMachineTypes() ([]string, error) {
	ctx := context.Background()
	instanceTypes, err := m.dcService.ListInstanceTypes(ctx)
	if err != nil {
		return nil, err
	}

	return instanceTypes, nil
}

// GetAvailableGPUTypes returns a map of available GPU types.
func (m *VerdacloudManager) GetAvailableGPUTypes() map[string]struct{} {
	ctx := context.Background()
	instanceTypes, err := m.dcService.ListInstanceTypes(ctx)
	if err != nil {
		klog.Warningf("GetAvailableGPUTypes: list instance types failed: %v", err)
		return nil
	}

	types := make(map[string]struct{}, len(instanceTypes))
	for _, instanceType := range instanceTypes {
		if strings.HasPrefix(strings.ToUpper(instanceType), "CPU.") {
			continue
		}
		types[instanceType] = struct{}{}
	}

	return types
}

func (m *VerdacloudManager) getInstancesForAsg(ref AsgRef) ([]cloudprovider.Instance, error) {
	asgInstances, err := m.asgs.InstancesForAsg(ref)
	if err != nil {
		return nil, err
	}
	cloudInstances := make([]cloudprovider.Instance, 0, len(asgInstances))
	for _, asgIns := range asgInstances {
		providerID := formatProviderID(asgIns.Location, asgIns.Hostname)
		status := strings.ToLower(asgIns.Status)
		switch status {
		// Offline VMs are group members whose Node the CCM keeps; reporting them as running lets scale-down remove them.
		case verda.StatusRunning, verda.StatusOffline:
			cloudInstances = append(cloudInstances, cloudprovider.Instance{
				Id: providerID,
				Status: &cloudprovider.InstanceStatus{
					State: cloudprovider.InstanceRunning,
				},
			})
		case verda.StatusNew, verda.StatusOrdered, verda.StatusProvisioning, verda.StatusValidating, verda.StatusPending:
			cloudInstances = append(cloudInstances, cloudprovider.Instance{
				Id: providerID,
				Status: &cloudprovider.InstanceStatus{
					State: cloudprovider.InstanceCreating,
				},
			})
		case verda.StatusDiscontinued, verda.StatusNotFound, verda.StatusDeleting:
			cloudInstances = append(cloudInstances, cloudprovider.Instance{Id: providerID, Status: &cloudprovider.InstanceStatus{State: cloudprovider.InstanceDeleting}})
		case verda.StatusError, verda.StatusNoCapacity, verda.StatusUnknown:
			// clusterstate only acts on ErrorInfo when State is InstanceCreating.
			cloudInstances = append(cloudInstances, cloudprovider.Instance{Id: providerID,
				Status: &cloudprovider.InstanceStatus{
					State: cloudprovider.InstanceCreating,
					ErrorInfo: &cloudprovider.InstanceErrorInfo{
						ErrorClass:   cloudprovider.OtherErrorClass,
						ErrorCode:    "verdacloud-instance-deployment-error",
						ErrorMessage: status,
					}}})
		default:
			cloudInstances = append(cloudInstances, cloudprovider.Instance{Id: providerID})
		}
	}
	return cloudInstances, nil
}

func (m *VerdacloudManager) buildNodeFromTemplate(asg *Asg, template *asgTemplate) (*apiv1.Node, error) {
	nodeName := fmt.Sprintf("asg-%s-%d", asg.Name, rand.Int63())
	nodeCfg := m.cfg.GetNodeConfig(asg.Name)

	// Kubelet-set labels, plus what the startup script passes via --node-labels.
	labels := map[string]string{
		"kubernetes.io/arch": template.InstanceType.Arch,
		"kubernetes.io/os":   "linux",
	}
	for k, v := range registeredNodeLabels(asg, nodeCfg) {
		labels[k] = v
	}
	// CCM-set labels (instance type from the API; region == zone == verda.com/location, lowercase).
	labels["node.kubernetes.io/instance-type"] = template.InstanceType.InstanceType
	for k, v := range ccmLocationLabels(asg.AvailabilityLocations) {
		labels[k] = v
	}

	capacity := apiv1.ResourceList{
		apiv1.ResourcePods:   *resource.NewQuantity(defaultPodAmountsLimit, resource.DecimalSI),
		apiv1.ResourceCPU:    *resource.NewQuantity(template.InstanceType.CPU, resource.DecimalSI),
		apiv1.ResourceMemory: *resource.NewQuantity(template.InstanceType.Memory, resource.BinarySI),
	}
	if template.InstanceType.GPU > 0 {
		capacity[apiv1.ResourceName(ResourceNvidiaGPU)] = *resource.NewQuantity(template.InstanceType.GPU, resource.DecimalSI)
	}

	node := &apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: nodeName, Labels: labels},
		Status: apiv1.NodeStatus{
			Capacity:    capacity,
			Allocatable: capacity,
			Conditions:  cloudprovider.BuildReadyConditions(),
		},
	}

	for k, v := range template.Tags {
		node.Labels[k] = v
	}
	node.Spec.Taints = append([]apiv1.Taint(nil), nodeCfg.Taints...)

	return node, nil
}

// registeredNodeLabels are the labels a real node of asg registers with (kubelet --node-labels).
// The template node carries the same set, so a pod that fits the template fits the real node.
// Provider keys win over config labels with the same key.
func registeredNodeLabels(asg *Asg, nodeCfg *nodeConfig) map[string]string {
	labels := make(map[string]string, len(nodeCfg.Labels)+2)
	for _, l := range nodeCfg.Labels {
		if k, v, ok := strings.Cut(l, "="); ok {
			labels[k] = v
		}
	}
	labels[NodeGroupLabelKey] = asg.Name
	if isGPUInstanceType(asg.instanceType) {
		labels[AcceleratorLabel] = asg.instanceType
	}
	return labels
}

// ccmLocationLabels mirrors the topology labels verdacloud-cloud-controller-manager sets.
// A multi-location group cannot know where its next VM lands, so it advertises none: claiming
// one would let a location-pinned pod trigger a scale-up that lands elsewhere.
func ccmLocationLabels(locations []string) map[string]string {
	if len(locations) != 1 {
		return nil
	}
	loc := strings.ToLower(locations[0])
	return map[string]string{
		apiv1.LabelTopologyRegion: loc,
		apiv1.LabelTopologyZone:   loc,
		LabelLocation:             loc,
	}
}

func (m *VerdacloudManager) getAsgTemplate(ctx context.Context, asgRef AsgRef) (*asgTemplate, error) {
	asg, err := m.asgs.GetAsgByRef(asgRef)
	if err != nil {
		return nil, err
	}

	instanceDetails, err := m.dcService.GetInstanceTypeDetails(ctx, asg.instanceType)
	if err != nil {
		return nil, fmt.Errorf("get instance type details for %s failed: %w", asg.instanceType, err)
	}

	return &asgTemplate{
		InstanceType: instanceDetails,
		Tags:         make(map[string]string),
	}, nil
}
