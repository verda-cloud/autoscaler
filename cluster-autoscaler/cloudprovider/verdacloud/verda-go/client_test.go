/*
Copyright The Kubernetes Authors.

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

package verda

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

const testToken = "REPLACE_ME_TOKEN"

// newTestClient serves /oauth2/token itself and routes everything else to api.
func newTestClient(t *testing.T, tokenTTLSeconds int, api http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()
	var tokenCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth2/token" {
			tokenCalls.Add(1)
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["grant_type"] != "client_credentials" || body["client_id"] != "id" {
				http.Error(w, `{"message":"bad token request"}`, http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": testToken, "expires_in": tokenTTLSeconds})
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("User-Agent") != "test-agent" {
			http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		api(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(WithClientID("id"), WithClientSecret("secret"), WithBaseURL(srv.URL+"/"), WithUserAgent("test-agent"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c, &tokenCalls
}

func TestNewClientRequiresCredentials(t *testing.T) {
	if _, err := NewClient(WithClientID("id")); err == nil {
		t.Fatal("NewClient without secret: want error, got nil")
	}
}

func TestTokenIsCachedUntilNearExpiry(t *testing.T) {
	list := func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`[]`)) }
	for _, tc := range []struct {
		name       string
		ttlSeconds int
		wantTokens int32
	}{
		{"long-lived token reused", 3600, 1},
		{"token inside expiry margin refetched", 10, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, tokenCalls := newTestClient(t, tc.ttlSeconds, list)
			for range 2 {
				if _, err := c.Instances.Get(context.Background(), ""); err != nil {
					t.Fatalf("Get: %v", err)
				}
			}
			if got := tokenCalls.Load(); got != tc.wantTokens {
				t.Fatalf("token requests = %d, want %d", got, tc.wantTokens)
			}
		})
	}
}

func TestCreateInstanceAcceptsJSONOrBareID(t *testing.T) {
	for _, tc := range []struct{ name, createBody string }{
		{"json body", `{"id":"i-1","hostname":"h"}`},
		{"bare id", `i-1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, 3600, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodPost && r.URL.Path == "/instances":
					var req map[string]any
					_ = json.NewDecoder(r.Body).Decode(&req)
					if _, ok := req["ssh_key_ids"].([]any); !ok {
						http.Error(w, `{"message":"ssh_key_ids must be a list"}`, http.StatusBadRequest)
						return
					}
					_, _ = w.Write([]byte(tc.createBody))
				case r.Method == http.MethodGet && r.URL.Path == "/instances/i-1":
					_, _ = w.Write([]byte(`{"id":"i-1","hostname":"h"}`))
				default:
					http.NotFound(w, r)
				}
			})
			inst, err := c.Instances.Create(context.Background(), CreateInstanceRequest{Hostname: "h"})
			if err != nil || inst.ID != "i-1" || inst.Hostname != "h" {
				t.Fatalf("Create = %+v, %v; want instance i-1", inst, err)
			}
		})
	}
}

func TestAddStartupScriptAcceptsQuotedBareID(t *testing.T) {
	c, _ := newTestClient(t, 3600, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`"s-1"`)) })
	script, err := c.StartupScripts.AddStartupScript(context.Background(), &CreateStartupScriptRequest{Name: "n", Script: "x"})
	if err != nil || script.ID != "s-1" {
		t.Fatalf("AddStartupScript = %+v, %v; want ID s-1", script, err)
	}
}

func TestInstanceActionResults(t *testing.T) {
	for _, tc := range []struct {
		name        string
		status      int
		body        string
		wantResults int
	}{
		{"204 has no results", http.StatusNoContent, ``, 0},
		{"207 per-instance results", http.StatusMultiStatus, `[{"instanceId":"i-1","status":"error","error":"boom"}]`, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, 3600, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			results, err := c.Instances.Action(context.Background(), InstanceActionRequest{Action: ActionDelete, ID: []string{"i-1"}})
			if err != nil || len(results) != tc.wantResults {
				t.Fatalf("Action = %d results, %v; want %d results", len(results), err, tc.wantResults)
			}
		})
	}
}

func TestAPIErrorBodies(t *testing.T) {
	for _, tc := range []struct{ name, body, wantCode, wantMessage string }{
		{"code and message", `{"code":"not_found","message":"no such instance"}`, "not_found", "no such instance"},
		{"message list", `{"message":["a","b"]}`, "", "a; b"},
		{"error field", `{"error":"invalid_client"}`, "", "invalid_client"},
		{"plain text", `gateway timeout`, "", "gateway timeout"},
		{"empty body", ``, "", "Not Found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, 3600, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := c.Instances.Get(context.Background(), "")
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if apiErr.StatusCode != http.StatusNotFound || apiErr.Code != tc.wantCode || apiErr.Message != tc.wantMessage {
				t.Fatalf("APIError = %+v, want status 404 code %q message %q", apiErr, tc.wantCode, tc.wantMessage)
			}
		})
	}
}

func TestAvailabilityQuery(t *testing.T) {
	c, _ := newTestClient(t, 3600, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/instance-availability/1H100.80S" || r.URL.Query().Get("is_spot") != "true" || r.URL.Query().Get("location_code") != "FIN-01" {
			http.Error(w, `{"message":"unexpected query"}`, http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`true`))
	})
	ok, err := c.InstanceAvailability.GetInstanceTypeAvailability(context.Background(), "1H100.80S", true, "FIN-01")
	if err != nil || !ok {
		t.Fatalf("GetInstanceTypeAvailability = %v, %v; want true", ok, err)
	}
}
