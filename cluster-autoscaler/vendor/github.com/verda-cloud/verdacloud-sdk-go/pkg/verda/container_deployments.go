package verda

import (
	"context"
	"fmt"
	"net/url"
)

type ContainerDeploymentsService struct {
	client *Client
}

// GetDeployments retrieves all container deployments
// projectID is optional - if empty, uses default project
func (s *ContainerDeploymentsService) GetDeployments(ctx context.Context) ([]ContainerDeployment, error) {
	path := "/container-deployments"

	// Note: projectId query parameter may be required by some API environments
	// The API typically uses the default project from authentication context
	// If you need explicit project support, use GetDeploymentsForProject

	deployments, _, err := getRequest[[]ContainerDeployment](ctx, s.client, path)
	if err != nil {
		return nil, err
	}
	return deployments, nil
}

// GetDeploymentsForProject retrieves container deployments for a specific project
func (s *ContainerDeploymentsService) GetDeploymentsForProject(ctx context.Context, projectID string) ([]ContainerDeployment, error) {
	path := "/container-deployments"

	if projectID != "" {
		params := url.Values{}
		params.Set("projectId", projectID)
		path += "?" + params.Encode()
	}

	deployments, _, err := getRequest[[]ContainerDeployment](ctx, s.client, path)
	if err != nil {
		return nil, err
	}
	return deployments, nil
}

func (s *ContainerDeploymentsService) CreateDeployment(ctx context.Context, req *CreateDeploymentRequest) (*ContainerDeployment, error) {
	if err := validateCreateDeploymentRequest(req); err != nil {
		return nil, err
	}

	deployment, _, err := postRequest[ContainerDeployment](ctx, s.client, "/container-deployments", req)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

// validateCreateDeploymentRequest validates all required fields for container deployment creation
func validateCreateDeploymentRequest(req *CreateDeploymentRequest) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}

	// Basic required fields
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if req.Compute.Name == "" {
		return fmt.Errorf("compute.name is required")
	}

	// Container validation
	if len(req.Containers) == 0 {
		return fmt.Errorf("at least one container is required")
	}
	for i, c := range req.Containers {
		if c.Image == "" {
			return fmt.Errorf("containers[%d].image is required", i)
		}
		// Check for "latest" tag - API does not allow it
		if isLatestTag(c.Image) {
			return fmt.Errorf("containers[%d].image: 'latest' tag is not allowed, please specify a specific version tag (e.g., nginx:1.25.3)", i)
		}
		if c.ExposedPort == 0 {
			return fmt.Errorf("containers[%d].exposed_port is required", i)
		}
	}

	// Scaling validation
	if req.Scaling.MaxReplicaCount == 0 {
		return fmt.Errorf("scaling.max_replica_count is required")
	}
	if req.Scaling.ScaleDownPolicy == nil {
		return fmt.Errorf("scaling.scale_down_policy is required")
	}
	if req.Scaling.ScaleUpPolicy == nil {
		return fmt.Errorf("scaling.scale_up_policy is required")
	}
	if req.Scaling.ScalingTriggers == nil {
		return fmt.Errorf("scaling.scaling_triggers is required")
	}
	if req.Scaling.ScalingTriggers.QueueLoad != nil && req.Scaling.ScalingTriggers.QueueLoad.Threshold < 1 {
		return fmt.Errorf("scaling.scaling_triggers.queue_load.threshold must be >= 1")
	}

	return nil
}

func (s *ContainerDeploymentsService) GetDeploymentByName(ctx context.Context, deploymentName string) (*ContainerDeployment, error) {
	path := fmt.Sprintf("/container-deployments/%s", deploymentName)
	deployment, _, err := getRequest[ContainerDeployment](ctx, s.client, path)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

func (s *ContainerDeploymentsService) UpdateDeployment(ctx context.Context, deploymentName string, req *UpdateDeploymentRequest) (*ContainerDeployment, error) {
	// Validate required fields for update
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}
	if deploymentName == "" {
		return nil, fmt.Errorf("deploymentName is required")
	}
	// Note: UpdateDeployment is a PATCH operation, so partial updates are allowed.
	// Containers are optional - you can update just scaling, compute, or other fields.

	path := fmt.Sprintf("/container-deployments/%s", deploymentName)
	deployment, _, err := patchRequest[ContainerDeployment](ctx, s.client, path, req)
	if err != nil {
		return nil, err
	}
	return &deployment, nil
}

// DeleteDeployment removes a deployment with timeout in milliseconds (0-300000ms)
// timeoutMs behavior:
//   - 0: Skip waiting (returns immediately)
//   - Negative (e.g., -1): Use API default of 60000ms (omit query parameter)
//   - 1-300000: Wait specified milliseconds
//   - >300000: Capped at 300000ms
func (s *ContainerDeploymentsService) DeleteDeployment(ctx context.Context, deploymentName string, timeoutMs int) error {
	if deploymentName == "" {
		return fmt.Errorf("deploymentName is required")
	}

	path := fmt.Sprintf("/container-deployments/%s", deploymentName)

	// Handle timeout parameter based on API specification
	// - Negative values: omit timeout parameter (API uses default 60000ms)
	// - 0: skip waiting (return immediately)
	// - 1-300000: explicit timeout value
	// - >300000: cap at maximum 300000ms
	if timeoutMs >= 0 {
		timeout := timeoutMs
		if timeout > 300000 {
			timeout = 300000 // cap at max 300 seconds
		}
		params := url.Values{}
		params.Set("timeout", fmt.Sprintf("%d", timeout))
		path += "?" + params.Encode()
	}
	// If timeoutMs < 0, don't add timeout parameter (use API default)

	_, err := deleteRequestAllowEmptyResponse(ctx, s.client, path)
	return err
}

func (s *ContainerDeploymentsService) GetDeploymentStatus(ctx context.Context, deploymentName string) (*ContainerDeploymentStatus, error) {
	path := fmt.Sprintf("/container-deployments/%s/status", deploymentName)
	status, _, err := getRequest[ContainerDeploymentStatus](ctx, s.client, path)
	if err != nil {
		return nil, err
	}
	return &status, nil
}

func (s *ContainerDeploymentsService) RestartDeployment(ctx context.Context, deploymentName string) error {
	path := fmt.Sprintf("/container-deployments/%s/restart", deploymentName)
	_, _, err := postRequest[interface{}](ctx, s.client, path, nil)
	return err
}

func (s *ContainerDeploymentsService) PauseDeployment(ctx context.Context, deploymentName string) error {
	path := fmt.Sprintf("/container-deployments/%s/pause", deploymentName)
	_, _, err := postRequest[interface{}](ctx, s.client, path, nil)
	return err
}

func (s *ContainerDeploymentsService) ResumeDeployment(ctx context.Context, deploymentName string) error {
	path := fmt.Sprintf("/container-deployments/%s/resume", deploymentName)
	_, _, err := postRequest[interface{}](ctx, s.client, path, nil)
	return err
}

func (s *ContainerDeploymentsService) PurgeDeploymentQueue(ctx context.Context, deploymentName string) error {
	path := fmt.Sprintf("/container-deployments/%s/purge-queue", deploymentName)
	_, _, err := postRequest[interface{}](ctx, s.client, path, nil)
	return err
}

func (s *ContainerDeploymentsService) GetDeploymentScaling(ctx context.Context, deploymentName string) (*ContainerScalingOptions, error) {
	if deploymentName == "" {
		return nil, fmt.Errorf("deploymentName is required")
	}
	path := fmt.Sprintf("/container-deployments/%s/scaling", deploymentName)
	scaling, _, err := getRequest[ContainerScalingOptions](ctx, s.client, path)
	if err != nil {
		return nil, err
	}
	return &scaling, nil
}

func (s *ContainerDeploymentsService) UpdateDeploymentScaling(ctx context.Context, deploymentName string, req *UpdateScalingOptionsRequest) (*ContainerScalingOptions, error) {
	if deploymentName == "" {
		return nil, fmt.Errorf("deploymentName is required")
	}
	if req == nil {
		return nil, fmt.Errorf("request cannot be nil")
	}
	path := fmt.Sprintf("/container-deployments/%s/scaling", deploymentName)
	scaling, _, err := patchRequest[ContainerScalingOptions](ctx, s.client, path, req)
	if err != nil {
		return nil, err
	}
	return &scaling, nil
}

func (s *ContainerDeploymentsService) GetDeploymentReplicas(ctx context.Context, deploymentName string) (*DeploymentReplicas, error) {
	path := fmt.Sprintf("/container-deployments/%s/replicas", deploymentName)
	replicas, _, err := getRequest[DeploymentReplicas](ctx, s.client, path)
	if err != nil {
		return nil, err
	}
	return &replicas, nil
}

func (s *ContainerDeploymentsService) GetEnvironmentVariables(ctx context.Context, deploymentName string) ([]ContainerEnvVar, error) {
	if deploymentName == "" {
		return nil, fmt.Errorf("deploymentName is required")
	}
	path := fmt.Sprintf("/container-deployments/%s/environment-variables", deploymentName)
	envVars, _, err := getRequest[[]ContainerEnvVar](ctx, s.client, path)
	if err != nil {
		return nil, err
	}
	return envVars, nil
}

func (s *ContainerDeploymentsService) AddEnvironmentVariables(ctx context.Context, deploymentName string, req *ContainerEnvVarsRequest) error {
	if deploymentName == "" {
		return fmt.Errorf("deploymentName is required")
	}
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}
	if req.ContainerName == "" {
		return fmt.Errorf("container_name is required")
	}
	if len(req.Env) == 0 {
		return fmt.Errorf("env array cannot be empty")
	}
	path := fmt.Sprintf("/container-deployments/%s/environment-variables", deploymentName)
	_, _, err := postRequest[interface{}](ctx, s.client, path, req)
	return err
}

func (s *ContainerDeploymentsService) UpdateEnvironmentVariables(ctx context.Context, deploymentName string, req *ContainerEnvVarsRequest) error {
	if deploymentName == "" {
		return fmt.Errorf("deploymentName is required")
	}
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}
	if req.ContainerName == "" {
		return fmt.Errorf("container_name is required")
	}
	if len(req.Env) == 0 {
		return fmt.Errorf("env array cannot be empty")
	}
	path := fmt.Sprintf("/container-deployments/%s/environment-variables", deploymentName)
	_, _, err := patchRequest[interface{}](ctx, s.client, path, req)
	return err
}

func (s *ContainerDeploymentsService) DeleteEnvironmentVariables(ctx context.Context, deploymentName string, req *DeleteContainerEnvVarsRequest) error {
	if deploymentName == "" {
		return fmt.Errorf("deploymentName is required")
	}
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}
	if req.ContainerName == "" {
		return fmt.Errorf("container_name is required")
	}
	if len(req.Env) == 0 {
		return fmt.Errorf("env array cannot be empty")
	}
	path := fmt.Sprintf("/container-deployments/%s/environment-variables", deploymentName)
	_, err := deleteRequestWithBody(ctx, s.client, path, req)
	return err
}

func (s *ContainerDeploymentsService) GetServerlessComputeResources(ctx context.Context) ([]ComputeResource, error) {
	resources, _, err := getRequest[[]ComputeResource](ctx, s.client, "/serverless-compute-resources")
	if err != nil {
		return nil, err
	}
	return resources, nil
}

func (s *ContainerDeploymentsService) GetSecrets(ctx context.Context) ([]Secret, error) {
	secrets, _, err := getRequest[[]Secret](ctx, s.client, "/secrets")
	if err != nil {
		return nil, err
	}
	return secrets, nil
}

func (s *ContainerDeploymentsService) CreateSecret(ctx context.Context, req *CreateSecretRequest) error {
	_, _, err := postRequest[interface{}](ctx, s.client, "/secrets", req)
	return err
}

// DeleteSecret removes a secret - force deletes even if in use (dangerous)
func (s *ContainerDeploymentsService) DeleteSecret(ctx context.Context, secretName string, force bool) error {
	path := fmt.Sprintf("/secrets/%s", secretName)

	if force {
		params := url.Values{}
		params.Set("force", "true")
		path += "?" + params.Encode()
	}

	_, err := deleteRequestAllowEmptyResponse(ctx, s.client, path)
	return err
}

func (s *ContainerDeploymentsService) GetFileSecrets(ctx context.Context) ([]FileSecret, error) {
	secrets, _, err := getRequest[[]FileSecret](ctx, s.client, "/file-secrets")
	if err != nil {
		return nil, err
	}
	return secrets, nil
}

func (s *ContainerDeploymentsService) CreateFileSecret(ctx context.Context, req *CreateFileSecretRequest) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(req.Files) == 0 {
		return fmt.Errorf("files map cannot be empty")
	}
	_, _, err := postRequest[interface{}](ctx, s.client, "/file-secrets", req)
	return err
}

func (s *ContainerDeploymentsService) DeleteFileSecret(ctx context.Context, secretName string, force bool) error {
	path := fmt.Sprintf("/file-secrets/%s", secretName)

	if force {
		params := url.Values{}
		params.Set("force", "true")
		path += "?" + params.Encode()
	}

	_, err := deleteRequestAllowEmptyResponse(ctx, s.client, path)
	return err
}

func (s *ContainerDeploymentsService) GetRegistryCredentials(ctx context.Context) ([]RegistryCredentials, error) {
	credentials, _, err := getRequest[[]RegistryCredentials](ctx, s.client, "/container-registry-credentials")
	if err != nil {
		return nil, err
	}
	return credentials, nil
}

func (s *ContainerDeploymentsService) CreateRegistryCredentials(ctx context.Context, req *CreateRegistryCredentialsRequest) error {
	if req == nil {
		return fmt.Errorf("request cannot be nil")
	}
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	if req.Type == "" {
		return fmt.Errorf("type is required")
	}
	_, _, err := postRequest[interface{}](ctx, s.client, "/container-registry-credentials", req)
	return err
}

func (s *ContainerDeploymentsService) DeleteRegistryCredentials(ctx context.Context, credentialsName string, force bool) error {
	path := fmt.Sprintf("/container-registry-credentials/%s", credentialsName)

	if force {
		params := url.Values{}
		params.Set("force", "true")
		path += "?" + params.Encode()
	}

	_, err := deleteRequestAllowEmptyResponse(ctx, s.client, path)
	return err
}
