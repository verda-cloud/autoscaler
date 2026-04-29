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
	"testing"
)

// TestTypeCacheRoundtrip verifies a value cached via cacheType is returned
// by lookupTypeCache, and that misses return nil.
func TestTypeCacheRoundtrip(t *testing.T) {
	w := newVerdacloudWrapper(nil)

	if got := w.lookupTypeCache("1H100.22V"); got != nil {
		t.Fatalf("empty cache should return nil, got %+v", got)
	}

	want := &InstanceResource{
		InstanceType: "1H100.22V",
		Arch:         "amd64",
		CPU:          22,
		Memory:       80 * 1024 * 1024 * 1024,
		GPU:          1,
	}
	w.cacheType("1H100.22V", want)

	got := w.lookupTypeCache("1H100.22V")
	if got != want {
		t.Fatalf("expected cached pointer back, got %+v", got)
	}
}

// TestTypeCacheCaseInsensitive verifies cache keys are case-folded so that
// callers using either VerdaCloud's canonical case or the upper-cased form
// in our config can both resolve cached entries.
func TestTypeCacheCaseInsensitive(t *testing.T) {
	w := newVerdacloudWrapper(nil)
	res := &InstanceResource{InstanceType: "1B300.30V", CPU: 30, Memory: 275 * 1024 * 1024 * 1024, GPU: 1}

	w.cacheType("1B300.30V", res)

	for _, key := range []string{"1B300.30V", "1b300.30v", "1B300.30v"} {
		if got := w.lookupTypeCache(key); got != res {
			t.Errorf("lookupTypeCache(%q) = %+v, want %+v", key, got, res)
		}
	}
}
