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
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	klog "k8s.io/klog/v2"
)

const (
	// instanceCacheTTL bounds instance cache freshness across autoscaler scan cycles.
	// Mutating operations invalidate the cache so subsequent reads observe fresh data.
	instanceCacheTTL = 1 * time.Minute
)

// instanceCache holds cached instance data with expiration
type instanceCache struct {
	instances []verda.Instance
	fetchedAt time.Time
	mu        sync.RWMutex
}

func (c *instanceCache) get() ([]verda.Instance, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.instances == nil || time.Since(c.fetchedAt) > instanceCacheTTL {
		return nil, false
	}
	// Return a copy to prevent mutation
	result := make([]verda.Instance, len(c.instances))
	copy(result, c.instances)
	return result, true
}

func (c *instanceCache) set(instances []verda.Instance) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.instances = make([]verda.Instance, len(instances))
	copy(c.instances, instances)
	c.fetchedAt = time.Now()
}

func (c *instanceCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.instances = nil
}

// dcService abstracts the VerdaCloud API for testability.
type dcService interface {
	ListInstancesCached(ctx context.Context) ([]verda.Instance, error)
	GetInstanceByHostname(ctx context.Context, hostname string) (*verda.Instance, error)
	GetActiveInstancesForAsg(ctx context.Context, asgName string, hostnamePrefix ...string) ([]verda.Instance, error)
	CreateInstance(ctx context.Context, req *verda.CreateInstanceRequest) (*verda.Instance, error)
	DeleteInstance(ctx context.Context, instanceID string) error
	PerformInstanceAction(ctx context.Context, instanceID, action string) error
	GetInstanceAvailabilityLocation(ctx context.Context, instanceType string, locations []string) (string, error)
	CreateStartScript(ctx context.Context, name, script string) (*verda.StartupScript, error)
	DeleteStartScript(ctx context.Context, id string) error
	ListInstanceTypes(ctx context.Context) ([]string, error)
	GetInstanceTypeDetails(ctx context.Context, instanceType string) (*InstanceResource, error)
	InvalidateCache()
}

type verdacloudWrapper struct {
	client *verda.Client
	cache  *instanceCache

	// typeCache stores last-known-good API responses for instance type
	// details, keyed by uppercased instance-type name. Served only when
	// a fresh API call fails. This protects against transient
	// /instance-types outages without ever fabricating data.
	typeCacheMu sync.RWMutex
	typeCache   map[string]*InstanceResource
}

func newVerdacloudWrapper(client *verda.Client) *verdacloudWrapper {
	return &verdacloudWrapper{
		client:    client,
		cache:     &instanceCache{},
		typeCache: make(map[string]*InstanceResource),
	}
}

func (w *verdacloudWrapper) lookupTypeCache(instanceType string) *InstanceResource {
	w.typeCacheMu.RLock()
	defer w.typeCacheMu.RUnlock()
	return w.typeCache[strings.ToUpper(instanceType)]
}

func (w *verdacloudWrapper) cacheType(instanceType string, res *InstanceResource) {
	w.typeCacheMu.Lock()
	defer w.typeCacheMu.Unlock()
	w.typeCache[strings.ToUpper(instanceType)] = res
}

func isActiveStatus(status string) bool {
	switch strings.ToLower(status) {
	case verda.StatusNew, verda.StatusOrdered, verda.StatusProvisioning,
		verda.StatusValidating, verda.StatusRunning, verda.StatusPending:
		return true
	}
	return false
}

func (w *verdacloudWrapper) GetInstanceAvailabilityLocation(ctx context.Context, instanceType string, locations []string) (string, error) {
	if len(locations) == 0 {
		return "", fmt.Errorf("locations is empty")
	}

	for _, loc := range locations {
		al := strings.ToUpper(loc)
		available, err := w.client.InstanceAvailability.GetInstanceTypeAvailability(ctx, instanceType, false, al)
		if err != nil {
			klog.V(5).Infof("Error checking availability for %s in %s: %v", instanceType, al, err)
			continue
		}
		if available {
			klog.V(4).Infof("Instance type %s available in %s", instanceType, al)
			return al, nil
		}
	}
	klog.Warningf("Instance type %s not available in any location", instanceType)
	return "", nil
}

// GetInstanceTypeDetails returns CPU/memory/GPU for the given instance type.
// Always uses exact API data and never heuristics. On API failure, falls back
// to the last-known-good API response from typeCache; returns an error only
// if we have never successfully observed the type.
func (w *verdacloudWrapper) GetInstanceTypeDetails(ctx context.Context, instanceType string) (*InstanceResource, error) {
	if instanceType == "" {
		return nil, fmt.Errorf("instance type is empty")
	}

	allInstanceTypes, err := w.client.InstanceTypes.Get(ctx, "")
	if err != nil {
		if cached := w.lookupTypeCache(instanceType); cached != nil {
			klog.Warningf("InstanceTypes API failed (%v); serving last-known-good cached details for %s", err, instanceType)
			return cached, nil
		}
		return nil, fmt.Errorf("fetch instance types: %w", err)
	}

	// API succeeded: refresh cache for every type the API returned, and
	// pick out the one the caller asked for.
	var found *InstanceResource
	for i := range allInstanceTypes {
		spec := &allInstanceTypes[i]
		res := &InstanceResource{
			InstanceType: spec.InstanceType,
			Arch:         "amd64",
			CPU:          int64(spec.CPU.NumberOfCores),
			Memory:       int64(spec.Memory.SizeInGigabytes) * 1024 * 1024 * 1024,
			GPU:          int64(spec.GPU.NumberOfGPUs),
		}
		w.cacheType(spec.InstanceType, res)
		if strings.EqualFold(spec.InstanceType, instanceType) {
			found = res
		}
	}

	if found != nil {
		return found, nil
	}
	return nil, fmt.Errorf("instance type %s not found in VerdaCloud API", instanceType)
}

// ListInstancesCached returns cached instances or fetches fresh data if cache expired.
func (w *verdacloudWrapper) ListInstancesCached(ctx context.Context) ([]verda.Instance, error) {
	if cached, ok := w.cache.get(); ok {
		klog.V(5).Info("Using cached instance list")
		return cached, nil
	}

	instances, err := w.client.Instances.Get(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list instances failed: %w", err)
	}

	w.cache.set(instances)
	klog.V(5).Infof("Fetched and cached %d instances", len(instances))
	return instances, nil
}

// InvalidateCache forces the next ListInstancesCached call to fetch fresh data.
func (w *verdacloudWrapper) InvalidateCache() {
	w.cache.invalidate()
}

func (w *verdacloudWrapper) GetInstanceByHostname(ctx context.Context, hostname string) (*verda.Instance, error) {
	instances, err := w.ListInstancesCached(ctx)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if strings.EqualFold(inst.Hostname, hostname) && isActiveStatus(inst.Status) {
			return &inst, nil
		}
	}
	return nil, fmt.Errorf("instance %s not found", hostname)
}

func (w *verdacloudWrapper) GetActiveInstancesForAsg(ctx context.Context, asgName string, hostnamePrefix ...string) ([]verda.Instance, error) {
	instances, err := w.ListInstancesCached(ctx)
	if err != nil {
		return nil, err
	}

	matchKey := asgName
	if len(hostnamePrefix) > 0 && hostnamePrefix[0] != "" {
		matchKey = hostnamePrefix[0]
	}

	filtered := make([]verda.Instance, 0)
	for _, inst := range instances {
		if !isActiveStatus(inst.Status) {
			continue
		}
		extractedPrefix, err := extractAsgNameFromHostname(inst.Hostname)
		if err != nil {
			continue
		}
		if strings.EqualFold(extractedPrefix, matchKey) {
			filtered = append(filtered, inst)
		}
	}

	klog.V(4).Infof("GetActiveInstancesForAsg %s: found %d instances", asgName, len(filtered))
	return filtered, nil
}

func (w *verdacloudWrapper) CreateInstance(ctx context.Context, req *verda.CreateInstanceRequest) (*verda.Instance, error) {
	instance, err := w.client.Instances.Create(ctx, *req)
	if err != nil {
		return nil, fmt.Errorf("create instance %s failed: %w", req.Hostname, err)
	}
	// Invalidate cache after creating instance
	w.InvalidateCache()
	klog.V(4).Infof("Created instance: hostname=%s, id=%s", req.Hostname, instance.ID)
	return instance, nil
}

func (w *verdacloudWrapper) PerformInstanceAction(ctx context.Context, instanceID, action string) error {
	_, err := w.client.Instances.Action(ctx, verda.InstanceActionRequest{
		Action: action,
		ID:     []string{instanceID},
	})
	if err == nil {
		// Invalidate cache after action that may change instance state
		w.InvalidateCache()
	}
	return err
}

func (w *verdacloudWrapper) DeleteInstance(ctx context.Context, instanceID string) error {
	if err := w.client.Instances.Delete(ctx, []string{instanceID}, nil, false); err != nil {
		return fmt.Errorf("delete instance %s failed: %w", instanceID, err)
	}
	// Invalidate cache after deletion
	w.InvalidateCache()
	klog.V(4).Infof("Deleted instance: id=%s", instanceID)
	return nil
}

func (w *verdacloudWrapper) CreateStartScript(ctx context.Context, name, script string) (*verda.StartupScript, error) {
	req := verda.CreateStartupScriptRequest{Name: name, Script: script}
	startupScript, err := w.client.StartupScripts.AddStartupScript(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("create startup script %s failed: %w", name, err)
	}
	return startupScript, nil
}

func (w *verdacloudWrapper) DeleteStartScript(ctx context.Context, id string) error {
	if err := w.client.StartupScripts.DeleteStartupScript(ctx, id); err != nil {
		klog.Warningf("delete startup script %s failed (script may leak on VerdaCloud side): %v", id, err)
		return err
	}
	return nil
}

func (w *verdacloudWrapper) ListInstanceTypes(ctx context.Context) ([]string, error) {
	availabilities, err := w.client.InstanceAvailability.GetAllAvailabilities(ctx, false, "")
	if err != nil {
		return nil, err
	}

	typeMap := make(map[string]bool)
	for _, avail := range availabilities {
		for _, instanceType := range avail.Availabilities {
			typeMap[instanceType] = true
		}
	}

	types := make([]string, 0, len(typeMap))
	for instanceType := range typeMap {
		types = append(types, instanceType)
	}

	return types, nil
}
