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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
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

// newHTTPTestWrapper serves the Verda API from handler (after a stub OAuth token endpoint)
// and returns a wrapper whose real SDK client talks to it.
func newHTTPTestWrapper(t *testing.T, handler http.HandlerFunc) *verdacloudWrapper {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"REPLACE_ME_TOKEN","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("/", handler)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	client, err := verda.NewClient(
		verda.WithClientID("REPLACE_ME_ID"),
		verda.WithClientSecret("REPLACE_ME_SECRET"),
		verda.WithBaseURL(srv.URL),
		verda.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatalf("verda.NewClient: %v", err)
	}
	return newVerdacloudWrapper(client)
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// deleteTestServer lists one instance with OS + data volumes and answers PUT /instances with
// actionStatus/actionBody, recording the decoded action request.
func deleteTestServer(t *testing.T, actionStatus int, actionBody any, got *verda.InstanceActionRequest) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/instances":
			writeJSON(t, w, http.StatusOK, []map[string]any{{
				"id": "i-1", "hostname": "pool-vm-fin-01-00000001", "status": "running",
				"os_volume_id": "os-1", "volume_ids": []string{"os-1", "data-1"},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/instances":
			if err := json.NewDecoder(r.Body).Decode(got); err != nil {
				t.Errorf("decode action request: %v", err)
			}
			writeJSON(t, w, actionStatus, actionBody)
		default:
			http.NotFound(w, r)
		}
	}
}

func TestWrapperDeleteInstance_DeletesOSAndAdditionalVolumes(t *testing.T) {
	var got verda.InstanceActionRequest
	w := newHTTPTestWrapper(t, deleteTestServer(t, http.StatusAccepted,
		[]verda.InstanceActionResult{{Action: "delete", InstanceID: "i-1", Status: "ok"}}, &got))

	if err := w.DeleteInstance(context.Background(), "i-1"); err != nil {
		t.Fatalf("DeleteInstance: %v", err)
	}
	if got.Action != verda.ActionDelete || !slices.Equal(got.ID, []string{"i-1"}) {
		t.Errorf("action request = %+v", got)
	}
	if !slices.Equal(got.VolumeIDs, []string{"os-1", "data-1"}) || !got.DeletePermanently {
		t.Errorf("volumes = %v deletePermanently = %t, want [os-1 data-1] true", got.VolumeIDs, got.DeletePermanently)
	}
}

func TestWrapperDeleteInstance_ResponseHandling(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    any
		wantErr bool
	}{
		{"207 with per-instance failure is an error", http.StatusMultiStatus,
			[]verda.InstanceActionResult{{InstanceID: "i-1", Error: "volume busy", StatusCode: http.StatusConflict}}, true},
		{"207 with per-instance 404 means already gone", http.StatusMultiStatus,
			[]verda.InstanceActionResult{{InstanceID: "i-1", Error: "not found", StatusCode: http.StatusNotFound}}, false},
		{"404 means already gone", http.StatusNotFound, map[string]string{"code": "not_found", "message": "no such instance"}, false},
		{"500 is an error", http.StatusInternalServerError, map[string]string{"code": "internal", "message": "boom"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got verda.InstanceActionRequest
			w := newHTTPTestWrapper(t, deleteTestServer(t, tc.status, tc.body, &got))
			if err := w.DeleteInstance(context.Background(), "i-1"); (err != nil) != tc.wantErr {
				t.Errorf("DeleteInstance err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestWrapperAvailability(t *testing.T) {
	t.Run("API error is returned, not reported as no capacity", func(t *testing.T) {
		w := newHTTPTestWrapper(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusInternalServerError, map[string]string{"code": "internal", "message": "boom"})
		})
		loc, err := w.GetInstanceAvailabilityLocation(context.Background(), "1H100.80S.22V", false, []string{"FIN-01"})
		if err == nil {
			t.Fatalf("expected error, got location %q", loc)
		}
	})

	t.Run("spot flag reaches the API and first available location wins", func(t *testing.T) {
		var spotParams []string
		w := newHTTPTestWrapper(t, func(w http.ResponseWriter, r *http.Request) {
			spotParams = append(spotParams, r.URL.Query().Get("is_spot"))
			writeJSON(t, w, http.StatusOK, r.URL.Query().Get("location_code") == "FIN-03")
		})
		loc, err := w.GetInstanceAvailabilityLocation(context.Background(), "1H100.80S.22V", true, []string{"fin-01", "fin-03"})
		if err != nil || loc != "FIN-03" {
			t.Fatalf("got (%q, %v), want (FIN-03, nil)", loc, err)
		}
		if !slices.Equal(spotParams, []string{"true", "true"}) {
			t.Errorf("is_spot params = %v, want [true true]", spotParams)
		}
	})

	t.Run("unavailable everywhere is empty without error", func(t *testing.T) {
		w := newHTTPTestWrapper(t, func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, http.StatusOK, false)
		})
		loc, err := w.GetInstanceAvailabilityLocation(context.Background(), "1H100.80S.22V", false, []string{"FIN-01"})
		if err != nil || loc != "" {
			t.Errorf("got (%q, %v), want (\"\", nil)", loc, err)
		}
	})
}

func TestWrapperListInstances_StopsAtCallerDeadline(t *testing.T) {
	w := newHTTPTestWrapper(t, func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // hang until the client gives up
	})
	const callerDeadline = 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), callerDeadline)
	defer cancel()

	start := time.Now()
	_, err := w.ListInstancesCached(ctx)
	if err == nil {
		t.Fatal("expected error from hung API")
	}
	if elapsed := time.Since(start); elapsed > 10*callerDeadline {
		t.Errorf("hung call returned after %v; the deadline was not propagated", elapsed)
	}
}
