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
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider/verdacloud/verda-go"
	klog "k8s.io/klog/v2"
)

const (
	// instanceCacheTTL bounds instance cache freshness across autoscaler scan cycles.
	// Mutating operations invalidate the cache so subsequent reads observe fresh data.
	instanceCacheTTL = 1 * time.Minute

	// apiCallTimeout bounds every Verda API call (including token refresh) so a hung
	// connection cannot stall Refresh() or hold an ASG's scaleMutex indefinitely.
	apiCallTimeout = 60 * time.Second
)

// withAPIDeadline derives the per-call context used for every Verda API request.
func withAPIDeadline(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, apiCallTimeout)
}

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
	CreateInstance(ctx context.Context, req *verda.CreateInstanceRequest) (*verda.Instance, error)
	// DeleteInstance deletes the VM together with its OS and additional volumes.
	// An instance the API reports as already gone (404) counts as deleted.
	DeleteInstance(ctx context.Context, instanceID string) error
	GetInstanceAvailabilityLocation(ctx context.Context, instanceType string, isSpot bool, locations []string) (string, error)
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

// isActiveStatus reports whether a VM counts as a node-group member. Offline VMs
// are members: the CCM keeps their Node (shutdown-tainted), so scale-down must be able to remove them.
func isActiveStatus(status string) bool {
	switch strings.ToLower(status) {
	case verda.StatusNew, verda.StatusOrdered, verda.StatusProvisioning,
		verda.StatusValidating, verda.StatusRunning, verda.StatusPending,
		verda.StatusOffline:
		return true
	}
	return false
}

// GetInstanceAvailabilityLocation returns the first location with capacity, or "" when every
// location answered "unavailable". API failures are returned, never reported as "no capacity".
func (w *verdacloudWrapper) GetInstanceAvailabilityLocation(ctx context.Context, instanceType string, isSpot bool, locations []string) (string, error) {
	if len(locations) == 0 {
		return "", fmt.Errorf("locations is empty")
	}

	var errs []error
	for _, loc := range locations {
		al := strings.ToUpper(loc)
		available, err := w.checkAvailability(ctx, instanceType, isSpot, al)
		if err != nil {
			errs = append(errs, fmt.Errorf("check availability of %s (spot=%t) in %s: %w", instanceType, isSpot, al, err))
			continue
		}
		if available {
			klog.V(4).Infof("Instance type %s (spot=%t) available in %s", instanceType, isSpot, al)
			return al, nil
		}
	}
	if len(errs) > 0 {
		return "", errors.Join(errs...)
	}
	klog.Warningf("Instance type %s (spot=%t) not available in any location", instanceType, isSpot)
	return "", nil
}

func (w *verdacloudWrapper) checkAvailability(ctx context.Context, instanceType string, isSpot bool, location string) (bool, error) {
	ctx, cancel := withAPIDeadline(ctx)
	defer cancel()
	return w.client.InstanceAvailability.GetInstanceTypeAvailability(ctx, instanceType, isSpot, location)
}

// GetInstanceTypeDetails returns CPU/memory/GPU for the given instance type.
// Always uses exact API data and never heuristics. On API failure, falls back
// to the last-known-good API response from typeCache; returns an error only
// if we have never successfully observed the type.
func (w *verdacloudWrapper) GetInstanceTypeDetails(ctx context.Context, instanceType string) (*InstanceResource, error) {
	if instanceType == "" {
		return nil, fmt.Errorf("instance type is empty")
	}

	ctx, cancel := withAPIDeadline(ctx)
	defer cancel()
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

	ctx, cancel := withAPIDeadline(ctx)
	defer cancel()
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

func (w *verdacloudWrapper) CreateInstance(ctx context.Context, req *verda.CreateInstanceRequest) (*verda.Instance, error) {
	ctx, cancel := withAPIDeadline(ctx)
	defer cancel()
	instance, err := w.client.Instances.Create(ctx, *req)
	if err != nil {
		return nil, fmt.Errorf("create instance %s failed: %w", req.Hostname, err)
	}
	// Invalidate cache after creating instance
	w.InvalidateCache()
	klog.V(4).Infof("Created instance: hostname=%s, id=%s", req.Hostname, instance.ID)
	return instance, nil
}

func (w *verdacloudWrapper) DeleteInstance(ctx context.Context, instanceID string) error {
	volumeIDs, err := w.volumeIDsOf(ctx, instanceID)
	if err != nil {
		return fmt.Errorf("delete instance %s: resolve volumes: %w", instanceID, err)
	}

	actionCtx, cancel := withAPIDeadline(ctx)
	defer cancel()
	results, err := w.client.Instances.Action(actionCtx, verda.InstanceActionRequest{
		Action:            verda.ActionDelete,
		ID:                []string{instanceID},
		VolumeIDs:         volumeIDs,
		DeletePermanently: true,
	})
	if err != nil {
		if isAPINotFound(err) {
			klog.V(4).Infof("Delete instance %s: already gone (404)", instanceID)
			w.InvalidateCache()
			return nil
		}
		return fmt.Errorf("delete instance %s failed: %w", instanceID, err)
	}
	if err := instanceActionError(instanceID, results); err != nil {
		return err
	}

	w.InvalidateCache()
	klog.V(4).Infof("Deleted instance: id=%s volumes=%v", instanceID, volumeIDs)
	return nil
}

// volumeIDsOf returns the OS and additional volume IDs attached to instanceID. Nil (API default:
// OS volume deleted, others detached) when the instance is not in the list.
func (w *verdacloudWrapper) volumeIDsOf(ctx context.Context, instanceID string) ([]string, error) {
	instances, err := w.ListInstancesCached(ctx)
	if err != nil {
		return nil, err
	}
	for _, inst := range instances {
		if inst.ID != instanceID {
			continue
		}
		var ids []string
		if inst.OSVolumeID != nil && *inst.OSVolumeID != "" {
			ids = append(ids, *inst.OSVolumeID)
		}
		for _, id := range inst.VolumeIDs {
			if id != "" && !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
		return ids, nil
	}
	klog.Warningf("Delete instance %s: not in instance list; additional volumes (if any) will be detached, not deleted", instanceID)
	return nil, nil
}

// instanceActionError converts a per-instance failure in a 202/207 action response into an error.
// A per-instance 404 means the instance is already gone and is not an error.
func instanceActionError(instanceID string, results []verda.InstanceActionResult) error {
	for _, r := range results {
		if r.InstanceID != instanceID {
			continue
		}
		if r.Error == "" && r.StatusCode < http.StatusBadRequest {
			return nil
		}
		if r.StatusCode == http.StatusNotFound {
			return nil
		}
		return fmt.Errorf("delete instance %s failed: status=%d error=%q", instanceID, r.StatusCode, r.Error)
	}
	return nil
}

func isAPINotFound(err error) bool {
	var apiErr *verda.APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

func (w *verdacloudWrapper) CreateStartScript(ctx context.Context, name, script string) (*verda.StartupScript, error) {
	ctx, cancel := withAPIDeadline(ctx)
	defer cancel()
	req := verda.CreateStartupScriptRequest{Name: name, Script: script}
	startupScript, err := w.client.StartupScripts.AddStartupScript(ctx, &req)
	if err != nil {
		return nil, fmt.Errorf("create startup script %s failed: %w", name, err)
	}
	return startupScript, nil
}

func (w *verdacloudWrapper) DeleteStartScript(ctx context.Context, id string) error {
	ctx, cancel := withAPIDeadline(ctx)
	defer cancel()
	if err := w.client.StartupScripts.DeleteStartupScript(ctx, id); err != nil {
		return fmt.Errorf("delete startup script %s: %w", id, err)
	}
	return nil
}

func (w *verdacloudWrapper) ListInstanceTypes(ctx context.Context) ([]string, error) {
	ctx, cancel := withAPIDeadline(ctx)
	defer cancel()
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
