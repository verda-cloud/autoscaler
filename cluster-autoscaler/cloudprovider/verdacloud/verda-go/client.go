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

// Package verda is a minimal Verda Cloud API client covering only the calls the
// cluster-autoscaler provider needs. It carries no third-party dependencies, per the
// cloudprovider policy on new dependencies.
package verda

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultBaseURL is the production Verda Cloud API endpoint.
const DefaultBaseURL = "https://api.verda.com/v1"

// tokenExpiryMargin treats a token as expired this long before its expiry, so a request never
// leaves with a token that lapses in flight.
const tokenExpiryMargin = 30 * time.Second

// maxResponseBytes bounds how much of a response body is read.
const maxResponseBytes = 16 << 20

// Client is a Verda Cloud API client. It is safe for concurrent use.
type Client struct {
	baseURL      string
	clientID     string
	clientSecret string
	userAgent    string
	httpClient   *http.Client

	// tokenMu guards token and tokenExpiry, and serializes token requests; the HTTP client's
	// timeout bounds how long a waiter can be held.
	tokenMu     sync.Mutex
	token       string
	tokenExpiry time.Time

	Instances            *InstanceService
	StartupScripts       *StartupScriptService
	InstanceAvailability *InstanceAvailabilityService
	InstanceTypes        *InstanceTypesService
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithClientID sets the OAuth client ID.
func WithClientID(id string) ClientOption { return func(c *Client) { c.clientID = id } }

// WithClientSecret sets the OAuth client secret.
func WithClientSecret(secret string) ClientOption {
	return func(c *Client) { c.clientSecret = secret }
}

// WithBaseURL overrides DefaultBaseURL.
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) { c.baseURL = strings.TrimRight(baseURL, "/") }
}

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(userAgent string) ClientOption {
	return func(c *Client) { c.userAgent = userAgent }
}

// WithHTTPClient sets the HTTP client; give it a timeout, since token requests use it too.
func WithHTTPClient(httpClient *http.Client) ClientOption {
	return func(c *Client) { c.httpClient = httpClient }
}

// NewClient returns a Client authenticating with the OAuth client credentials grant.
func NewClient(options ...ClientOption) (*Client, error) {
	c := &Client{baseURL: DefaultBaseURL, httpClient: http.DefaultClient}
	for _, option := range options {
		option(c)
	}
	if c.clientID == "" || c.clientSecret == "" {
		return nil, errors.New("verda client ID and client secret are required")
	}
	c.Instances = &InstanceService{client: c}
	c.StartupScripts = &StartupScriptService{client: c}
	c.InstanceAvailability = &InstanceAvailabilityService{client: c}
	c.InstanceTypes = &InstanceTypesService{client: c}
	return c, nil
}

// bearerToken returns a valid access token, requesting a new one when the cached one is near expiry.
func (c *Client) bearerToken(ctx context.Context) (string, error) {
	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()
	if c.token != "" && time.Now().Add(tokenExpiryMargin).Before(c.tokenExpiry) {
		return c.token, nil
	}

	body, err := json.Marshal(map[string]string{
		"grant_type":    "client_credentials",
		"client_id":     c.clientID,
		"client_secret": c.clientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("encode token request: %w", err)
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if _, _, err := c.send(ctx, http.MethodPost, "/oauth2/token", body, "", &resp); err != nil {
		return "", fmt.Errorf("verda authentication: %w", err)
	}
	if resp.AccessToken == "" {
		return "", errors.New("verda authentication: response has no access_token")
	}
	c.token = resp.AccessToken
	c.tokenExpiry = time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	return c.token, nil
}

// do sends an authenticated JSON request and decodes a JSON response into out (if non-nil).
// It returns the HTTP status and raw body so callers can handle the API's non-JSON replies.
func (c *Client) do(ctx context.Context, method, path string, in, out any) (int, []byte, error) {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return 0, nil, fmt.Errorf("encode %s %s: %w", method, path, err)
		}
	}
	token, err := c.bearerToken(ctx)
	if err != nil {
		return 0, nil, err
	}
	return c.send(ctx, method, path, body, token, out)
}

func (c *Client) send(ctx context.Context, method, path string, body []byte, token string, out any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("build %s %s: %w", method, path, err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("read %s %s response: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp.StatusCode, respBody, parseAPIError(resp.StatusCode, respBody)
	}
	if out != nil && len(bytes.TrimSpace(respBody)) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return resp.StatusCode, respBody, fmt.Errorf("decode %s %s response: %w", method, path, err)
		}
	}
	return resp.StatusCode, respBody, nil
}

// parseAPIError reads the API's {"code","message"} or {"error"} bodies; any other body is kept as text.
func parseAPIError(status int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: status}
	var envelope struct {
		Code    string          `json:"code"`
		Message json.RawMessage `json:"message"`
		Error   string          `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		apiErr.Code = envelope.Code
		var list []string
		if json.Unmarshal(envelope.Message, &list) == nil {
			apiErr.Message = strings.Join(list, "; ")
		} else if json.Unmarshal(envelope.Message, &apiErr.Message) != nil || apiErr.Message == "" {
			apiErr.Message = envelope.Error
		}
	}
	if apiErr.Message == "" {
		apiErr.Message = strings.TrimSpace(string(body))
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(status)
	}
	return apiErr
}

// plainTextID reads the bare ID that some create endpoints return instead of JSON.
func plainTextID(body []byte) string {
	return strings.Trim(strings.TrimSpace(string(body)), `"`)
}

// InstanceService manages instances.
type InstanceService struct{ client *Client }

// Get lists instances, optionally filtered by status.
func (s *InstanceService) Get(ctx context.Context, status string) ([]Instance, error) {
	path := "/instances"
	if status != "" {
		path += "?" + url.Values{"status": {status}}.Encode()
	}
	var instances []Instance
	_, _, err := s.client.do(ctx, http.MethodGet, path, nil, &instances)
	return instances, err
}

// GetByID returns one instance.
func (s *InstanceService) GetByID(ctx context.Context, id string) (*Instance, error) {
	var instance Instance
	if _, _, err := s.client.do(ctx, http.MethodGet, "/instances/"+url.PathEscape(id), nil, &instance); err != nil {
		return nil, err
	}
	return &instance, nil
}

// Create deploys an instance. The API answers with either the instance or its bare ID.
func (s *InstanceService) Create(ctx context.Context, req CreateInstanceRequest) (*Instance, error) {
	if req.SSHKeyIDs == nil {
		req.SSHKeyIDs = []string{}
	}
	_, body, err := s.client.do(ctx, http.MethodPost, "/instances", req, nil)
	if err != nil {
		return nil, err
	}
	var instance Instance
	if json.Unmarshal(body, &instance) == nil && instance.ID != "" {
		return &instance, nil
	}
	return s.GetByID(ctx, plainTextID(body))
}

// Action performs an action on instances. A 204 returns no results; 202 and 207 return
// per-instance results, which may carry per-instance errors.
func (s *InstanceService) Action(ctx context.Context, req InstanceActionRequest) ([]InstanceActionResult, error) {
	status, body, err := s.client.do(ctx, http.MethodPut, "/instances", req, nil)
	if err != nil || status == http.StatusNoContent {
		return nil, err
	}
	var results []InstanceActionResult
	if len(bytes.TrimSpace(body)) > 0 {
		if err := json.Unmarshal(body, &results); err != nil {
			return nil, fmt.Errorf("decode instance action results (status %d): %w", status, err)
		}
	}
	return results, nil
}

// StartupScriptService manages startup scripts.
type StartupScriptService struct{ client *Client }

// AddStartupScript stores a startup script. The API answers with either the script or its bare ID.
func (s *StartupScriptService) AddStartupScript(ctx context.Context, req *CreateStartupScriptRequest) (*StartupScript, error) {
	_, body, err := s.client.do(ctx, http.MethodPost, "/scripts", req, nil)
	if err != nil {
		return nil, err
	}
	var script StartupScript
	if json.Unmarshal(body, &script) == nil && script.ID != "" {
		return &script, nil
	}
	id := plainTextID(body)
	if id == "" {
		return nil, errors.New("create startup script: empty response")
	}
	return &StartupScript{ID: id, Name: req.Name, Script: req.Script}, nil
}

// DeleteStartupScript deletes a startup script.
func (s *StartupScriptService) DeleteStartupScript(ctx context.Context, id string) error {
	_, _, err := s.client.do(ctx, http.MethodDelete, "/scripts/"+url.PathEscape(id), nil, nil)
	return err
}

// InstanceAvailabilityService queries capacity.
type InstanceAvailabilityService struct{ client *Client }

func availabilityQuery(isSpot bool, locationCode string) string {
	params := url.Values{}
	if isSpot {
		params.Set("is_spot", "true")
	}
	if locationCode != "" {
		params.Set("location_code", locationCode)
	}
	if len(params) == 0 {
		return ""
	}
	return "?" + params.Encode()
}

// GetAllAvailabilities lists available instance types per location.
func (s *InstanceAvailabilityService) GetAllAvailabilities(ctx context.Context, isSpot bool, locationCode string) ([]LocationAvailability, error) {
	var availabilities []LocationAvailability
	_, _, err := s.client.do(ctx, http.MethodGet, "/instance-availability"+availabilityQuery(isSpot, locationCode), nil, &availabilities)
	return availabilities, err
}

// GetInstanceTypeAvailability reports whether an instance type can be deployed in a location now.
func (s *InstanceAvailabilityService) GetInstanceTypeAvailability(ctx context.Context, instanceType string, isSpot bool, locationCode string) (bool, error) {
	var available bool
	path := "/instance-availability/" + url.PathEscape(instanceType) + availabilityQuery(isSpot, locationCode)
	_, _, err := s.client.do(ctx, http.MethodGet, path, nil, &available)
	return available, err
}

// InstanceTypesService lists instance types.
type InstanceTypesService struct{ client *Client }

// Get lists instance types, optionally priced in currency.
func (s *InstanceTypesService) Get(ctx context.Context, currency string) ([]InstanceTypeInfo, error) {
	path := "/instance-types"
	if currency != "" {
		path += "?" + url.Values{"currency": {currency}}.Encode()
	}
	var types []InstanceTypeInfo
	_, _, err := s.client.do(ctx, http.MethodGet, path, nil, &types)
	return types, err
}
