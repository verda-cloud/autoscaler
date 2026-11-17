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
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"k8s.io/autoscaler/cluster-autoscaler/cloudprovider/verdacloud/verda-go"
	klog "k8s.io/klog/v2"
)

// ASG constants for hostname parsing and capacity management.
const (
	// ASG_SEPARATOR_MAGIC_NUMBER is used in hostnames: {prefix}-vm-{location}-{random}.
	ASG_SEPARATOR_MAGIC_NUMBER = "vm"

	FAILED_INSTANCE_BACKOFF_DURATION  = 5 * time.Minute  // Wait before retrying after terminal failures.
	FAILED_INSTANCE_CLEANUP_AGE       = 10 * time.Minute // Delete stuck failed instances after this age.
	FAILED_INSTANCE_MAP_ENTRY_TTL     = 1 * time.Hour    // Prevent unbounded map growth.
	MAX_CONCURRENT_INSTANCE_CREATIONS = 10               // Limit concurrent provider API calls.

	// pendingInstanceGracePeriod is how long a VM we created may be missing from the
	// instance list (create→list lag) before it is treated as gone.
	pendingInstanceGracePeriod = 5 * time.Minute

	// maxHostnameAttempts bounds regeneration of a random hostname suffix on collision.
	maxHostnameAttempts = 5

	// ownershipMarkerPrefix starts the VM Description written by ownershipMarker.
	ownershipMarkerPrefix = "verda-ca"
)

// ASG_SEPARATOR is the separator pattern used to parse hostnames.
var ASG_SEPARATOR = fmt.Sprintf("-%s-", ASG_SEPARATOR_MAGIC_NUMBER)

// autoScalingGroups stores ASG instance state. Mutable fields are protected by
// cacheMutex unless a narrower lock is documented by the caller.
//
// Node lifecycle (orphan-Node deletion when a VM disappears) is delegated to
// verdacloud-cloud-controller-manager — its InstancesV2 returning
// (false, nil) on a missing VM triggers the upstream node-controller to
// delete the Node. The autoscaler does not list or delete K8s Nodes itself.
type autoScalingGroups struct {
	registeredAsgs    map[AsgRef]*Asg          // All known/managed ASGs (by ref)
	asgToInstances    map[AsgRef][]InstanceRef // ASG to VM refs
	instanceToAsg     map[InstanceRef]*Asg     // VM ref to ASG pointer
	instanceIDs       map[InstanceRef]string   // Ref to API instance ID
	asgNodeGroupSpecs map[AsgRef]string        // ASG to original spec string
	cfg               *cloudConfig             // Global config pointer (ownership)
	dcService         dcService                // VerdaCloud API wrapper/interface

	failedInstances  map[string]time.Time // Hostname to first failure time.
	lastFailureCheck map[AsgRef]time.Time // ASG to last observed failed instance time.

	// pendingCreates holds VMs created by this process that the instance list has not shown
	// yet, keyed by hostname. Entries leave on first sighting or after pendingInstanceGracePeriod.
	pendingCreates map[string]pendingInstance

	cacheMutex sync.RWMutex // Concurrency - covers all above mutable fields
}

// pendingInstance is a VM this process created that the API list has not shown yet.
type pendingInstance struct {
	ref       InstanceRef
	asgRef    AsgRef
	id        string
	createdAt time.Time
}

// newAutoScalingGroups validates and registers node group specs.
func newAutoScalingGroups(dcService dcService, nodeGroupSpecs []string, cfg *cloudConfig) (*autoScalingGroups, error) {
	registry := &autoScalingGroups{
		registeredAsgs:    make(map[AsgRef]*Asg),
		asgToInstances:    make(map[AsgRef][]InstanceRef),
		instanceToAsg:     make(map[InstanceRef]*Asg),
		instanceIDs:       make(map[InstanceRef]string),
		asgNodeGroupSpecs: make(map[AsgRef]string),
		failedInstances:   make(map[string]time.Time),
		lastFailureCheck:  make(map[AsgRef]time.Time),
		pendingCreates:    make(map[string]pendingInstance),
		cfg:               cfg,
		dcService:         dcService,
	}

	// Validate all declared ASG specs before returning the registry.
	if err := registry.parseASGNodeGroupSpecs(nodeGroupSpecs); err != nil {
		return nil, err
	}

	return registry, nil
}

// Get all registered ASGs (as pointers), concurrency-safe.
func (m *autoScalingGroups) getAsgs() []*Asg {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()
	asgs := make([]*Asg, 0, len(m.registeredAsgs))
	for _, asg := range m.registeredAsgs {
		asgs = append(asgs, asg)
	}
	return asgs
}

// Get an ASG by its AsgRef key. Returns error if not present.
func (m *autoScalingGroups) GetAsgByRef(ref AsgRef) (*Asg, error) {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()
	asg, exists := m.registeredAsgs[ref]
	if !exists {
		return nil, fmt.Errorf("ASG not found for ref: %s", ref.Name)
	}
	return asg, nil
}

// FindASGForInstance attempts to find the ASG for a given instance reference.
//   - Direct map by ref is fast path.
//   - Fallback: match by hostname (handles old/canonicalization mismatches).
func (m *autoScalingGroups) FindASGForInstance(ref *InstanceRef) (*Asg, error) {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()

	if asg, exists := m.instanceToAsg[*ref]; exists {
		return asg, nil
	}
	// Defensive: handle variant ProviderID encodings by also matching only on hostname.
	for cachedRef, asg := range m.instanceToAsg {
		if cachedRef.Hostname == ref.Hostname {
			return asg, nil
		}
	}
	klog.V(4).Infof("Instance %s not found in cache", ref.Hostname)
	return nil, nil
}

// Helper: Find cached InstanceRef and associated ASG for the given hostname.
//
//	Callers MUST hold cacheMutex.
func (m *autoScalingGroups) findCachedRefByHostname(hostname string) (*InstanceRef, *Asg) {
	for ref, asg := range m.instanceToAsg {
		if ref.Hostname == hostname {
			return &ref, asg
		}
	}
	return nil, nil
}

// Main refresh logic: Pull latest VM state from API; update local cache accordingly.
// This is at the heart of the ASG/provider lifecycle.
// Concurrency note: cache swap and curSize rebalancing is atomic.
func (m *autoScalingGroups) regenerate() error {
	ctx := context.Background()

	// (1) List all known instances from the backend API
	allInstances, err := m.dcService.ListInstancesCached(ctx)
	if err != nil {
		return fmt.Errorf("failed to list instances: %w", err)
	}

	// (2) Snapshot current state under read lock. This allows intelligent comparison of cache vs live.
	existingAsgs, existingAsgToInstances, pendingCreates := func() (map[AsgRef]*Asg, map[AsgRef][]InstanceRef, map[string]pendingInstance) {
		m.cacheMutex.RLock()
		defer m.cacheMutex.RUnlock()
		ea := make(map[AsgRef]*Asg, len(m.registeredAsgs))
		for ref, asg := range m.registeredAsgs {
			ea[ref] = asg
		}
		eai := make(map[AsgRef][]InstanceRef, len(m.asgToInstances))
		for ref, instances := range m.asgToInstances {
			eai[ref] = append([]InstanceRef(nil), instances...)
		}
		pc := make(map[string]pendingInstance, len(m.pendingCreates))
		for hostname, p := range m.pendingCreates {
			pc[hostname] = p
		}
		return ea, eai, pc
	}()

	// (3) Produce new cache sets based on API state. Track failed instances per ASG for later bookkeeping.
	now := time.Now()
	newCache := &instanceMaps{
		instanceToAsg:  make(map[InstanceRef]*Asg),
		asgToInstances: make(map[AsgRef][]InstanceRef),
		instanceIDs:    make(map[InstanceRef]string),
	}
	allFailedInstances := make(map[AsgRef][]verda.Instance)
	droppedByAsg := make(map[AsgRef]int)
	allSeenHostnames := make(map[string]bool)

	// Sweep each ASG, record active & failed, and assign to new cache.
	for _, asg := range existingAsgs {
		activeInstances, failedInstances, apiSeenHostnames := m.categorizeInstancesForAsg(allInstances, asg)
		allFailedInstances[asg.AsgRef] = failedInstances
		for hostname := range apiSeenHostnames {
			allSeenHostnames[hostname] = true
		}

		// Add active to cache
		for _, inst := range activeInstances {
			ref := InstanceRef{
				Hostname:   inst.Hostname,
				ProviderID: formatProviderID(inst.Location, inst.Hostname),
			}
			newCache.addInstance(ref, asg, inst.ID)
		}

		// Carry over instances we created that the API does not list yet.
		preservePendingInstances(asg, apiSeenHostnames, pendingCreates, now, newCache)
		droppedByAsg[asg.AsgRef] = countDroppedMembers(existingAsgToInstances[asg.AsgRef], failedInstances, newCache)
	}

	// (4) Atomically install new cache and reconcile curSize according to observed state
	m.cacheMutex.Lock()
	m.instanceToAsg = newCache.instanceToAsg
	m.asgToInstances = newCache.asgToInstances
	m.instanceIDs = newCache.instanceIDs
	m.prunePendingCreates(allSeenHostnames, now)
	m.reconcileCurSize(newCache.asgToInstances, allFailedInstances, droppedByAsg)
	m.cacheMutex.Unlock()

	// (5) Process failed/provisioning-failed instances: update backoff tracking & cleanup stuck ones.
	m.processFailedInstances(existingAsgs, allFailedInstances)

	return nil
}

// Reconciliation routine: Bumps curSize within each ASG.
// Rule: If more active seen than curSize, adopt them as real (manual creation).
//
//	Members that vanished or reached a non-member status shrink curSize (never below active).
//	If fewer and there are failures, drop curSize to match active.
//	Otherwise, maintain optimism as provisioning can be delayed.
//
// Must be called with cacheMutex held!
func (m *autoScalingGroups) reconcileCurSize(asgToInstances map[AsgRef][]InstanceRef, failedByAsg map[AsgRef][]verda.Instance, droppedByAsg map[AsgRef]int) {
	for ref, asg := range m.registeredAsgs {
		activeCount := len(asgToInstances[ref])
		failedCount := len(failedByAsg[ref])

		if dropped := droppedByAsg[ref]; dropped > 0 && asg.curSize > activeCount {
			newSize := max(asg.curSize-dropped, activeCount)
			klog.Warningf("ASG %s curSize: %d -> %d (%d members no longer listed as active)",
				asg.Name, asg.curSize, newSize, dropped)
			asg.curSize = newSize
		}

		if activeCount > asg.curSize {
			klog.V(4).Infof("ASG %s curSize: %d -> %d (active instances increased)",
				asg.Name, asg.curSize, activeCount)
			asg.curSize = activeCount
		} else if failedCount > 0 && asg.curSize > activeCount {
			klog.Warningf("ASG %s: curSize %d -> %d (detected %d failed instances)",
				asg.Name, asg.curSize, activeCount, failedCount)
			asg.curSize = activeCount
		}
	}
}

// instanceMaps groups recomputed cache maps passed between regenerate phases.
type instanceMaps struct {
	instanceToAsg  map[InstanceRef]*Asg
	asgToInstances map[AsgRef][]InstanceRef
	instanceIDs    map[InstanceRef]string
}

// addInstance adds a discovered instance to all instance maps.
func (c *instanceMaps) addInstance(ref InstanceRef, asg *Asg, instanceID string) {
	c.instanceToAsg[ref] = asg
	c.asgToInstances[asg.AsgRef] = append(c.asgToInstances[asg.AsgRef], ref)
	if instanceID != "" {
		c.instanceIDs[ref] = instanceID
	}
}

// Categorize all API instances by active/failed for this ASG only.
// Returns also full list of matching hostnames observed in live API for this ASG.
// Only VMs this cluster owns (see ownsInstance) are considered.
func (m *autoScalingGroups) categorizeInstancesForAsg(allInstances []verda.Instance, asg *Asg) (active, failed []verda.Instance, seenHostnames map[string]bool) {
	matchKey := asg.hostnamePrefix
	if matchKey == "" {
		matchKey = asg.Name
	}

	seenHostnames = make(map[string]bool)
	for _, inst := range allInstances {
		prefix, err := extractAsgNameFromHostname(inst.Hostname)
		if err != nil || !strings.EqualFold(prefix, matchKey) {
			continue
		}
		if !m.ownsInstance(inst, asg) {
			klog.V(4).Infof("Skipping instance %s: description %q does not mark it as owned by cluster %q",
				inst.Hostname, inst.Description, m.cfg.ClusterName)
			continue
		}
		seenHostnames[inst.Hostname] = true
		if isActiveStatus(inst.Status) {
			active = append(active, inst)
		} else if isProvisioningFailedStatus(inst.Status) {
			failed = append(failed, inst)
		}
	}
	return
}

// ownershipMarker is the VM Description identifying the owning cluster and group.
func ownershipMarker(clusterName, asgName string) string {
	return fmt.Sprintf("%s cluster=%s group=%s", ownershipMarkerPrefix, clusterName, asgName)
}

// parseOwnershipMarker extracts the cluster from a Description written by ownershipMarker.
func parseOwnershipMarker(description string) (cluster string, ok bool) {
	fields := strings.Fields(description)
	if len(fields) == 0 || fields[0] != ownershipMarkerPrefix {
		return "", false
	}
	for _, field := range fields[1:] {
		if value, found := strings.CutPrefix(field, "cluster="); found {
			return value, true
		}
	}
	return "", false
}

// ownsInstance: a marked VM must name this cluster; an unmarked VM is legacy and adopted only when
// its Description is empty or the group name (what earlier versions wrote).
func (m *autoScalingGroups) ownsInstance(inst verda.Instance, asg *Asg) bool {
	if cluster, marked := parseOwnershipMarker(inst.Description); marked {
		return cluster == m.cfg.ClusterName
	}
	return inst.Description == "" || inst.Description == asg.Name
}

// preservePendingInstances keeps VMs we created within pendingInstanceGracePeriod that the
// API does not list yet, so in-flight nodes are not forgotten. Anything else missing from a
// successful list is gone and is not carried over.
func preservePendingInstances(asg *Asg, apiSeenHostnames map[string]bool, pending map[string]pendingInstance, now time.Time, new *instanceMaps) {
	for hostname, p := range pending {
		if p.asgRef != asg.AsgRef || apiSeenHostnames[hostname] {
			continue
		}
		if now.Sub(p.createdAt) > pendingInstanceGracePeriod {
			klog.Warningf("ASG %s: instance %s (id=%s) not listed %v after creation; treating as gone",
				asg.Name, hostname, p.id, pendingInstanceGracePeriod)
			continue
		}
		if _, exists := new.instanceToAsg[p.ref]; !exists {
			new.addInstance(p.ref, asg, p.id)
		}
	}
}

// countDroppedMembers counts previous members absent from the new cache, excluding failed
// instances (reconcileCurSize accounts for those separately).
func countDroppedMembers(previous []InstanceRef, failed []verda.Instance, new *instanceMaps) int {
	failedHostnames := make(map[string]bool, len(failed))
	for _, inst := range failed {
		failedHostnames[inst.Hostname] = true
	}
	dropped := 0
	for _, ref := range previous {
		if _, kept := new.instanceToAsg[ref]; kept || failedHostnames[ref.Hostname] {
			continue
		}
		if keptByHostname(new, ref.Hostname) {
			continue
		}
		dropped++
	}
	return dropped
}

// keptByHostname matches refs whose ProviderID changed form (e.g. a placeholder that gained its API location).
func keptByHostname(c *instanceMaps, hostname string) bool {
	for ref := range c.instanceToAsg {
		if ref.Hostname == hostname {
			return true
		}
	}
	return false
}

// prunePendingCreates drops entries the API has listed or that outlived the grace period.
// Must be called with cacheMutex held.
func (m *autoScalingGroups) prunePendingCreates(seenHostnames map[string]bool, now time.Time) {
	for hostname, p := range m.pendingCreates {
		if seenHostnames[hostname] || now.Sub(p.createdAt) > pendingInstanceGracePeriod {
			delete(m.pendingCreates, hostname)
		}
	}
}

// Return true if provisioning status marks a terminal (won't proceed to ready) failure.
// This MUST cover all backend statuses where auto-retry is safe.
func isProvisioningFailedStatus(status string) bool {
	switch strings.ToLower(status) {
	case verda.StatusNoCapacity, verda.StatusError, verda.StatusUnknown:
		return true
	}
	return false
}

// processFailedInstances tracks failed nodes and removes persistent failures.
func (m *autoScalingGroups) processFailedInstances(registeredAsgs map[AsgRef]*Asg, failedByAsg map[AsgRef][]verda.Instance) {
	now := time.Now()

	// Clean up old tracking entries that exceed their TTL.
	m.cacheMutex.Lock()
	for hostname, markedTime := range m.failedInstances {
		if now.Sub(markedTime) > FAILED_INSTANCE_MAP_ENTRY_TTL {
			delete(m.failedInstances, hostname)
		}
	}
	m.cacheMutex.Unlock()

	// Main logic, per-ASG scan
	for asgRef, asg := range registeredAsgs {
		failedInstances := failedByAsg[asgRef]
		if len(failedInstances) == 0 {
			continue
		}
		for _, inst := range failedInstances {
			m.trackAndCleanupFailedInstance(inst, now)
		}
		m.cacheMutex.Lock()
		m.lastFailureCheck[asgRef] = now
		m.cacheMutex.Unlock()
		klog.Warningf("ASG %s has %d failed instances, backoff until %v", asg.Name, len(failedInstances), now.Add(FAILED_INSTANCE_BACKOFF_DURATION))
	}
}

// Track a failed instance.
// - First failure will set a timestamp in map.
// - If persistent for too long, will attempt to delete via API.
func (m *autoScalingGroups) trackAndCleanupFailedInstance(inst verda.Instance, now time.Time) {
	markedTime, exists := func() (time.Time, bool) {
		m.cacheMutex.Lock()
		defer m.cacheMutex.Unlock()
		t, ok := m.failedInstances[inst.Hostname]
		if !ok {
			m.failedInstances[inst.Hostname] = now
		}
		return t, ok
	}()
	if !exists {
		klog.Warningf("Found failed instance: %s (status: %s)", inst.Hostname, inst.Status)
		return
	}

	if now.Sub(markedTime) > FAILED_INSTANCE_CLEANUP_AGE {
		ctx := context.Background()
		if err := m.dcService.DeleteInstance(ctx, inst.ID); err != nil {
			klog.Errorf("Failed to delete failed instance %s: %v", inst.Hostname, err)
		} else {
			m.cacheMutex.Lock()
			delete(m.failedInstances, inst.Hostname)
			m.cacheMutex.Unlock()
			klog.Infof("Deleted old failed instance %s (status was: %s)", inst.Hostname, inst.Status)
		}
	}
}

// Build ASG object from a node group spec string (as supplied in config). Validates asgSpec.
func (m *autoScalingGroups) buildASGFromSpec(spec string) (*Asg, error) {
	asgSpec, err := parseAsgSpec(spec)
	if err != nil {
		return nil, err
	}

	nodeConfig := m.cfg.GetNodeConfig(asgSpec.name)
	return &Asg{
		AsgRef:                AsgRef{Name: asgSpec.name},
		minSize:               asgSpec.minSize,
		maxSize:               asgSpec.maxSize,
		instanceType:          strings.ToUpper(asgSpec.instanceType),
		hostnamePrefix:        asgSpec.hostnamePrefix,
		AvailabilityLocations: nodeConfig.AvailableLocations,
	}, nil
}

// Parse and register each node group spec into the ASG map.
// Fails on first invalid spec.
func (m *autoScalingGroups) parseASGNodeGroupSpecs(specs []string) error {
	hostnameBaseOwner := make(map[string]string) // effective hostname base -> ASG name
	for _, spec := range specs {
		asg, err := m.buildASGFromSpec(spec)
		if err != nil {
			return err
		}
		if _, dup := m.registeredAsgs[asg.AsgRef]; dup {
			return fmt.Errorf("duplicate ASG name %q in node group specs", asg.Name)
		}
		base := asg.hostnameBase()
		if owner, clash := hostnameBaseOwner[base]; clash {
			return fmt.Errorf("ASGs %q and %q both generate hostnames starting with %q; VMs could not be told apart", owner, asg.Name, base+ASG_SEPARATOR)
		}
		hostnameBaseOwner[base] = asg.Name
		m.registeredAsgs[asg.AsgRef] = asg
		m.asgNodeGroupSpecs[asg.AsgRef] = spec
	}
	klog.V(4).Infof("Registered %d ASGs", len(m.asgNodeGroupSpecs))
	return nil
}

// Atomically increments ASG curSize, with overflow protection.
// Returns new size or error if would exceed maxSize.
func (m *autoScalingGroups) incrementTargetSize(asg *Asg, delta int) (int, error) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	if asg.curSize+delta > asg.maxSize {
		return 0, fmt.Errorf("size increase is too large - desired:%d max:%d",
			asg.curSize+delta, asg.maxSize)
	}

	asg.curSize += delta
	return asg.curSize, nil
}

// Adjust the ASG curSize by delta value. No bounds check; intended for rollback adjustment.
func (m *autoScalingGroups) adjustTargetSize(asg *Asg, delta int) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()
	asg.curSize += delta
}

// Result of provisioning new instances: reference and resulting instance ID.
type instanceCreateResult struct {
	ref InstanceRef
	id  string
}

// Update cache for given ASG with freshly provisioned instances.
// Intended for use after successful CreateInstances.
func (m *autoScalingGroups) updateCacheWithInstances(asg *Asg, results []instanceCreateResult) {
	m.cacheMutex.Lock()
	defer m.cacheMutex.Unlock()

	now := time.Now()
	for _, r := range results {
		m.instanceToAsg[r.ref] = asg
		m.asgToInstances[asg.AsgRef] = append(m.asgToInstances[asg.AsgRef], r.ref)
		m.instanceIDs[r.ref] = r.id
		m.pendingCreates[r.ref.Hostname] = pendingInstance{ref: r.ref, asgRef: asg.AsgRef, id: r.id, createdAt: now}
	}
}

// Core scale-up logic for ASG: Validates, concurrency-protects, provisions via API, and fully updates state.
func (m *autoScalingGroups) scaleUpAsg(asg *Asg, delta int) error {
	if delta <= 0 {
		return fmt.Errorf("size increase must be positive")
	}

	ctx := context.Background()

	// scaleMutex is held across API calls; each call is bounded by apiCallTimeout.
	asg.scaleMutex.Lock()
	defer asg.scaleMutex.Unlock()

	m.cacheMutex.RLock()
	curSize := asg.curSize
	m.cacheMutex.RUnlock()
	klog.Infof("Scale-up ASG %s by %d (curSize=%d, max=%d)", asg.Name, delta, curSize, asg.maxSize)

	// Implement backoff after terminal failures (NoCapacity, Error, etc).
	if err := m.checkFailureBackoff(asg); err != nil {
		return err
	}

	newSize, err := m.incrementTargetSize(asg, delta)
	if err != nil {
		return err
	}

	rollbackNeeded := true
	defer func() {
		if rollbackNeeded {
			m.adjustTargetSize(asg, -delta)
		}
	}()

	nodeConfig, err := m.getNodeConfigForAsg(asg)
	if err != nil {
		return fmt.Errorf("get node config failed: %w", err)
	}

	// Find an eligible location for this ASG instance type (provider affinity)
	location, err := m.dcService.GetInstanceAvailabilityLocation(ctx, asg.instanceType, nodeConfig.IsSpot, asg.AvailabilityLocations)
	if err != nil {
		return fmt.Errorf("availability check failed: %w", err)
	}
	if location == "" {
		return fmt.Errorf("instance type %s not available", asg.instanceType)
	}

	rollbackNeeded = false // partial success from here is tolerated

	results, errs := m.createInstances(ctx, asg, nodeConfig, location, delta)
	if len(results) > 0 {
		m.updateCacheWithInstances(asg, results)
	}

	failedCount := delta - len(results)
	if failedCount > 0 {
		m.adjustTargetSize(asg, -failedCount)
	}

	klog.Infof("Scale-up ASG %s complete: created %d/%d instances (curSize=%d)", asg.Name, len(results), delta, newSize)

	if len(errs) > 0 {
		return fmt.Errorf("failed to create %d/%d instances: %w", len(errs), delta, errors.Join(errs...))
	}
	return nil
}

// Check if the ASG is still in backoff from recent NoCapacity/Error failures, and how much longer if so.
func (m *autoScalingGroups) checkFailureBackoff(asg *Asg) error {
	m.cacheMutex.RLock()
	lastCheck, exists := m.lastFailureCheck[asg.AsgRef]
	m.cacheMutex.RUnlock()

	if exists && time.Since(lastCheck) < FAILED_INSTANCE_BACKOFF_DURATION {
		remaining := FAILED_INSTANCE_BACKOFF_DURATION - time.Since(lastCheck)
		return fmt.Errorf("failure backoff active for %v", remaining)
	}
	return nil
}

// Concurrent creation of instances.
// Uses a semaphore to limit concurrent outstanding CreateInstance calls (provider rate-limit safety).
func (m *autoScalingGroups) createInstances(ctx context.Context, asg *Asg, nodeConfig *nodeConfig, location string, count int) ([]instanceCreateResult, []error) {
	sem := make(chan struct{}, MAX_CONCURRENT_INSTANCE_CREATIONS)
	var wg sync.WaitGroup
	resultsCh := make(chan instanceCreateResult, count)
	errsCh := make(chan error, count)

	for i := 0; i < count; i++ {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			instanceID, hostname, err := m.createInstanceForAsg(ctx, asg, nodeConfig, location)
			if err != nil {
				errsCh <- err
				return
			}
			resultsCh <- instanceCreateResult{
				ref: InstanceRef{
					Hostname:   hostname,
					ProviderID: formatProviderID(location, hostname),
				},
				id: instanceID,
			}
		}()
	}

	wg.Wait()
	close(resultsCh)
	close(errsCh)

	var results []instanceCreateResult
	var errs []error
	for r := range resultsCh {
		results = append(results, r)
	}
	for err := range errsCh {
		errs = append(errs, err)
	}
	return results, errs
}

// Returns the nodeConfig for an ASG, with correct image type (CPU/GPU generic override).
func (m *autoScalingGroups) getNodeConfigForAsg(asg *Asg) (*nodeConfig, error) {
	nodeConfig := m.cfg.GetNodeConfig(asg.Name)

	isGPU := isGPUInstanceType(asg.instanceType)
	if isGPU {
		nodeConfig.Image = m.cfg.Image.GPU
	} else {
		nodeConfig.Image = m.cfg.Image.CPU
	}

	return nodeConfig, nil
}

// Actually issues the API call to provision an instance for the ASG at location.
// Handles all env/label/script patching, cleans up script on exit.
func (m *autoScalingGroups) createInstanceForAsg(ctx context.Context, asg *Asg, nodeConfig *nodeConfig, location string) (string, string, error) {
	hostname, err := m.newUniqueHostname(ctx, asg, location)
	if err != nil {
		return "", "", err
	}

	klog.V(4).Infof("Creating instance %s with providerID=%s", hostname, formatProviderID(location, hostname))

	startupScriptID, err := m.createStartupScript(ctx, asg, nodeConfig)
	if err != nil {
		return "", hostname, fmt.Errorf("create startup script failed: %w", err)
	}
	if startupScriptID == "" {
		return "", hostname, errors.New("startup script creation returned empty ID")
	}
	// Deleted once the create call returns (API copies the script on create). The rendered script
	// holds the join token, so a failed delete is logged at error level rather than ignored.
	defer func() {
		if err := m.dcService.DeleteStartScript(ctx, startupScriptID); err != nil {
			klog.Errorf("Startup script %s for %s was not deleted and still contains the cluster join token; delete it manually: %v",
				startupScriptID, hostname, err)
		}
	}()

	if nodeConfig.Image == "" {
		return "", hostname, fmt.Errorf("no image configured for instance type %s", asg.instanceType)
	}

	input := verda.CreateInstanceRequest{
		InstanceType:    asg.instanceType,
		Image:           nodeConfig.Image,
		SSHKeyIDs:       nodeConfig.SSHKeyIDs,
		StartupScriptID: &startupScriptID,
		Hostname:        hostname,
		Description:     ownershipMarker(m.cfg.ClusterName, asg.Name),
		LocationCode:    location,
		IsSpot:          nodeConfig.IsSpot,
		Contract:        nodeConfig.Contract,
		Pricing:         nodeConfig.Price,
		OSVolume: &verda.OSVolumeCreateRequest{
			Name: fmt.Sprintf("%s-os-volume", hostname),
			Size: nodeConfig.OSVolumeSize,
		},
	}
	if nodeConfig.IsSpot {
		input.OSVolume.OnSpotDiscontinue = verda.SpotDiscontinueDeletePermanent
	}

	if len(nodeConfig.Volumes) > 0 {
		input.Volumes = make([]verda.VolumeCreateRequest, len(nodeConfig.Volumes))
		for i, vol := range nodeConfig.Volumes {
			input.Volumes[i] = verda.VolumeCreateRequest{Name: vol.Name, Size: vol.Size, Type: vol.Type}
			if nodeConfig.IsSpot {
				input.Volumes[i].OnSpotDiscontinue = verda.SpotDiscontinueDeletePermanent
			}
		}
	}

	instance, err := m.dcService.CreateInstance(ctx, &input)
	if err != nil {
		return "", hostname, fmt.Errorf("create instance failed: %w", err)
	}

	klog.V(4).Infof("Created instance %s (id=%s) for ASG %s", hostname, instance.ID, asg.Name)
	return instance.ID, hostname, nil
}

// newUniqueHostname returns {base}-vm-{location}-{random} not used by any known VM.
func (m *autoScalingGroups) newUniqueHostname(ctx context.Context, asg *Asg, location string) (string, error) {
	instances, err := m.dcService.ListInstancesCached(ctx)
	if err != nil {
		return "", fmt.Errorf("list instances for hostname uniqueness: %w", err)
	}
	taken := make(map[string]bool, len(instances))
	for _, inst := range instances {
		taken[strings.ToLower(inst.Hostname)] = true
	}
	m.cacheMutex.RLock()
	for ref := range m.instanceToAsg {
		taken[strings.ToLower(ref.Hostname)] = true
	}
	m.cacheMutex.RUnlock()

	for range maxHostnameAttempts {
		hostname := fmt.Sprintf("%s%s%s-%08x", asg.hostnameBase(), ASG_SEPARATOR, strings.ToLower(location), rand.Uint32())
		if !taken[hostname] {
			return hostname, nil
		}
	}
	return "", fmt.Errorf("could not generate an unused hostname for ASG %s after %d attempts", asg.Name, maxHostnameAttempts)
}

// createStartupScript decodes the cloud-config startup script, renders it with env-backed and per-group template data, and uploads to Verda.
func (m *autoScalingGroups) createStartupScript(ctx context.Context, asg *Asg, nodeConfig *nodeConfig) (string, error) {
	scriptName := fmt.Sprintf("as-%s", asg.Name)
	decodedScript, err := base64.StdEncoding.DecodeString(nodeConfig.StartupScript)
	if err != nil {
		return "", fmt.Errorf("failed to decode startup script: %v", err)
	}

	vars := StartupScriptTemplateData{
		NodeLabels: kubeletNodeLabelsFlag(registeredNodeLabels(asg, nodeConfig)),
		NodeTaints: kubeletTaintsFlag(nodeConfig.Taints),
	}
	if m.cfg != nil {
		vars.MasterIP = m.cfg.MasterIP
		vars.MasterPort = m.cfg.MasterPort
		vars.JoinToken = m.cfg.JoinToken
		vars.JoinHashFull = m.cfg.JoinHashFull
	}
	rendered, err := renderStartupScript(decodedScript, vars)
	if err != nil {
		return "", fmt.Errorf("render startup script: %w", err)
	}

	script, err := m.dcService.CreateStartScript(ctx, scriptName, string(rendered))
	if err != nil {
		klog.Errorf("CreateStartScript API call failed: %v", err)
		return "", fmt.Errorf("failed to create startup script: %v", err)
	}

	klog.V(4).Infof("Created startup script %s (id=%s)", scriptName, script.ID)
	return script.ID, nil
}

// Remove an instance from cache. Must hold cacheMutex when calling.
// If host not present, no-op. Also decrements ASG.curSize.
func (m *autoScalingGroups) removeInstanceFromCache(hostname string, asg *Asg) {
	ref, _ := m.findCachedRefByHostname(hostname)
	if ref == nil {
		return
	}
	delete(m.instanceToAsg, *ref)
	delete(m.instanceIDs, *ref)
	delete(m.pendingCreates, hostname)
	refs := m.asgToInstances[asg.AsgRef]
	for i, r := range refs {
		if r.Hostname == hostname {
			m.asgToInstances[asg.AsgRef] = append(refs[:i], refs[i+1:]...)
			break
		}
	}
	asg.curSize--
}

// Return a list of InstanceRefs for a given ASG (by ref), concurrency-safe.
func (m *autoScalingGroups) InstanceRefsForAsg(ref AsgRef) ([]InstanceRef, error) {
	m.cacheMutex.RLock()
	defer m.cacheMutex.RUnlock()
	return m.asgToInstances[ref], nil
}

// Returns full verda.Instance objects for all known VMs in the given ASG.
// If not in API yet, fills dummy object with StatusOrdered.
func (m *autoScalingGroups) InstancesForAsg(ref AsgRef) ([]verda.Instance, error) {
	ctx := context.Background()

	m.cacheMutex.RLock()
	refs := append([]InstanceRef(nil), m.asgToInstances[ref]...)
	m.cacheMutex.RUnlock()

	allInstances, err := m.dcService.ListInstancesCached(ctx)
	if err != nil {
		return nil, err
	}

	apiByHostname := make(map[string]verda.Instance, len(allInstances))
	for _, inst := range allInstances {
		apiByHostname[inst.Hostname] = inst
	}

	instances := make([]verda.Instance, 0, len(refs))
	for _, r := range refs {
		if inst, ok := apiByHostname[r.Hostname]; ok {
			instances = append(instances, inst)
		} else {
			// Not yet visible in API (ordered state), provide placeholder for autoscaler logic.
			// Location comes from the ref so the placeholder's providerID matches what the CCM writes.
			instances = append(instances, verda.Instance{
				ID:       r.ProviderID,
				Hostname: r.Hostname,
				Location: locationFromProviderID(r.ProviderID),
				Status:   verda.StatusOrdered,
			})
		}
	}
	return instances, nil
}

// Delete an entire ASG: Performs bulk removal of all associated instances, then unregisters ASG.
// Note: Each instance is removed via API (concurrently, rate-limited). Waits for all deletions.
// Returns error if *any* fails; logs skipped if no instance ID known.
func (m *autoScalingGroups) DeleteAsg(ref AsgRef) error {
	ctx := context.Background()

	m.cacheMutex.RLock()
	instanceRefs := append([]InstanceRef(nil), m.asgToInstances[ref]...)
	asg := m.registeredAsgs[ref]
	// Prepare IDs for deletion (reduce lookup inside goroutine)
	idsToDelete := make(map[string]string, len(instanceRefs)) // hostname to instance ID
	for _, insRef := range instanceRefs {
		if id, ok := m.instanceIDs[insRef]; ok {
			idsToDelete[insRef.Hostname] = id
		}
	}
	m.cacheMutex.RUnlock()

	var wg sync.WaitGroup
	errsCh := make(chan error, len(instanceRefs))
	sem := make(chan struct{}, MAX_CONCURRENT_INSTANCE_CREATIONS)
	for _, insRef := range instanceRefs {
		instanceID, ok := idsToDelete[insRef.Hostname]
		if !ok {
			klog.Warningf("DeleteAsg: no cached instance ID for %s, skipping", insRef.Hostname)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(id, hostname string) {
			defer func() { <-sem; wg.Done() }()
			if err := m.dcService.DeleteInstance(ctx, id); err != nil {
				errsCh <- fmt.Errorf("delete %s failed: %w", hostname, err)
			}
		}(instanceID, insRef.Hostname)
	}
	wg.Wait()
	close(errsCh)

	var errs []error
	for err := range errsCh {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to delete all instances: %w", errors.Join(errs...))
	}

	// Remove ASG and all its members from cache under lock.
	m.cacheMutex.Lock()
	for _, insRef := range instanceRefs {
		delete(m.instanceToAsg, insRef)
		delete(m.instanceIDs, insRef)
	}
	delete(m.registeredAsgs, ref)
	delete(m.asgToInstances, ref)
	delete(m.asgNodeGroupSpecs, ref)
	if asg != nil {
		asg.curSize = 0
	}
	m.cacheMutex.Unlock()
	return nil
}

// Delete a single instance by reference. Handles lookup and cache clean up.
// Returns error if instance not found or no known instance ID.
func (m *autoScalingGroups) DeleteInstance(ref InstanceRef) error {
	ctx := context.Background()

	m.cacheMutex.RLock()
	asg, found := m.instanceToAsg[ref]
	if !found {
		cachedRef, cachedAsg := m.findCachedRefByHostname(ref.Hostname)
		if cachedRef != nil {
			ref = *cachedRef
			asg = cachedAsg
			found = true
		}
	}
	instanceID := m.instanceIDs[ref]
	curSizeAtRead := 0
	if asg != nil {
		curSizeAtRead = asg.curSize
	}
	m.cacheMutex.RUnlock()

	if !found {
		return fmt.Errorf("instance %s not found in any ASG", ref.Hostname)
	}
	if instanceID == "" {
		return fmt.Errorf("no cached instance ID for %s", ref.Hostname)
	}
	if m.dcService == nil {
		return fmt.Errorf("dcService is not initialized")
	}

	klog.V(4).Infof("DeleteInstance: deleting %s (id=%s, curSize: %d)",
		ref.Hostname, instanceID, curSizeAtRead)

	if err := m.dcService.DeleteInstance(ctx, instanceID); err != nil {
		return fmt.Errorf("delete instance %s failed: %w", ref.Hostname, err)
	}

	m.cacheMutex.Lock()
	m.removeInstanceFromCache(ref.Hostname, asg)
	m.cacheMutex.Unlock()

	return nil
}
