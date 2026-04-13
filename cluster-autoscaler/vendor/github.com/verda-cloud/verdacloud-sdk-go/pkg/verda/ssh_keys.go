package verda

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type SSHKeyService struct {
	client *Client
}

type CreateSSHKeyRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"key"`
}

type DeleteMultipleSSHKeysRequest struct {
	Keys []string `json:"keys"`
}

func (s *SSHKeyService) GetAllSSHKeys(ctx context.Context) ([]SSHKey, error) {
	keys, _, err := getRequest[[]SSHKey](ctx, s.client, "/ssh-keys")
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func (s *SSHKeyService) GetSSHKeyByID(ctx context.Context, sshKeyID string) (*SSHKey, error) {
	path := fmt.Sprintf("/ssh-keys/%s", sshKeyID)

	// API returns array even for single key lookup
	keys, _, err := getRequest[[]SSHKey](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	if len(keys) == 0 {
		return nil, fmt.Errorf("SSH key with ID %s not found", sshKeyID)
	}

	return &keys[0], nil
}

// AddSSHKey creates a key and refetches it since the API returns only the ID as plain text
func (s *SSHKeyService) AddSSHKey(ctx context.Context, req *CreateSSHKeyRequest) (*SSHKey, error) {
	return s.createWithPlainTextResponse(ctx, req)
}

func (s *SSHKeyService) createWithPlainTextResponse(ctx context.Context, req *CreateSSHKeyRequest) (*SSHKey, error) {
	resp, err := s.client.makeRequest(ctx, http.MethodPost, "/ssh-keys", req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read error response: %w", err)
		}
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	keyID := strings.TrimSpace(string(body))
	return s.GetSSHKeyByID(ctx, keyID)
}

func (s *SSHKeyService) DeleteSSHKey(ctx context.Context, sshKeyID string) error {
	path := fmt.Sprintf("/ssh-keys/%s", sshKeyID)
	_, err := deleteRequestAllowEmptyResponse(ctx, s.client, path)
	return err
}

func (s *SSHKeyService) DeleteMultipleSSHKeys(ctx context.Context, keyIDs []string) error {
	req := &DeleteMultipleSSHKeysRequest{
		Keys: keyIDs,
	}
	_, err := deleteRequestWithBody(ctx, s.client, "/ssh-keys", req)
	return err
}
