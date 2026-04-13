package verda

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type VolumeService struct {
	client *Client
}

func (s *VolumeService) ListVolumes(ctx context.Context) ([]Volume, error) {
	return s.ListVolumesByStatus(ctx, "")
}

func (s *VolumeService) ListVolumesByStatus(ctx context.Context, status string) ([]Volume, error) {
	path := "/volumes"
	if status != "" {
		params := url.Values{}
		params.Set("status", status)
		path += "?" + params.Encode()
	}

	volumes, _, err := getRequest[[]Volume](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return volumes, nil
}

func (s *VolumeService) GetVolume(ctx context.Context, id string) (*Volume, error) {
	path := fmt.Sprintf("/volumes/%s", id)
	volume, _, err := getRequest[Volume](ctx, s.client, path)
	if err != nil {
		return nil, err
	}
	return &volume, nil
}

func (s *VolumeService) CreateVolume(ctx context.Context, req VolumeCreateRequest) (string, error) {
	if req.LocationCode == "" {
		req.LocationCode = LocationFIN01
	}

	return s.createVolumeWithPlainTextResponse(ctx, req)
}

func (s *VolumeService) createVolumeWithPlainTextResponse(ctx context.Context, req VolumeCreateRequest) (string, error) {
	resp, err := s.client.makeRequest(ctx, http.MethodPost, "/volumes", req)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", s.client.handleResponse(resp, nil)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	volumeID := strings.TrimSpace(string(body))
	return volumeID, nil
}

func (s *VolumeService) DeleteVolume(ctx context.Context, id string, force bool) error {
	path := fmt.Sprintf("/volumes/%s", id)
	if force {
		path += "?force=true"
	}

	_, err := deleteRequestAllowEmptyResponse(ctx, s.client, path)
	return err
}

// AttachVolume attaches a volume - instance must be shut down first
func (s *VolumeService) AttachVolume(ctx context.Context, volumeID string, req VolumeAttachRequest) error {
	// Use map to combine action fields with instance_id
	payload := map[string]interface{}{
		"id":          volumeID,
		"action":      VolumeActionAttach,
		"instance_id": req.InstanceID,
	}
	_, err := putRequestAllowEmptyResponse(ctx, s.client, "/volumes", payload)
	return err
}

// DetachVolume detaches a volume - instance must be shut down first
func (s *VolumeService) DetachVolume(ctx context.Context, volumeID string, req VolumeDetachRequest) error {
	payload := map[string]interface{}{
		"id":          volumeID,
		"action":      VolumeActionDetach,
		"instance_id": req.InstanceID,
	}
	_, err := putRequestAllowEmptyResponse(ctx, s.client, "/volumes", payload)
	return err
}

// CloneVolume clones a volume and returns the new volume ID
func (s *VolumeService) CloneVolume(ctx context.Context, volumeID string, req VolumeCloneRequest) (string, error) {
	actionReq := VolumeActionRequest{
		ID:     volumeID,
		Action: VolumeActionClone,
		Name:   req.Name,
		Type:   req.LocationCode, // Note: Python SDK uses 'type' field for location
	}

	resp, err := s.client.makeRequest(ctx, http.MethodPut, "/volumes", actionReq)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", s.client.handleResponse(resp, nil)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// API returns an array of volume IDs, get the first one
	var volumeIDs []string
	if err := json.Unmarshal(body, &volumeIDs); err != nil {
		// If not an array, try as single string
		newVolumeID := strings.TrimSpace(string(body))
		return newVolumeID, nil
	}

	if len(volumeIDs) > 0 {
		return volumeIDs[0], nil
	}

	return "", fmt.Errorf("no volume ID returned from clone operation")
}

// ResizeVolume grows a volume - shrinking is not supported
func (s *VolumeService) ResizeVolume(ctx context.Context, volumeID string, req VolumeResizeRequest) error {
	actionReq := VolumeActionRequest{
		ID:     volumeID,
		Action: VolumeActionResize,
		Size:   req.Size,
	}
	_, err := putRequestAllowEmptyResponse(ctx, s.client, "/volumes", actionReq)
	return err
}

func (s *VolumeService) RenameVolume(ctx context.Context, volumeID string, req VolumeRenameRequest) error {
	actionReq := VolumeActionRequest{
		ID:     volumeID,
		Action: VolumeActionRename,
		Name:   req.Name,
	}
	_, err := putRequestAllowEmptyResponse(ctx, s.client, "/volumes", actionReq)
	return err
}
