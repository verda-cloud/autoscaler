package verda

import (
	"fmt"
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// ContainerDeployment represents a serverless container deployment
type ContainerDeployment struct {
	Name                      string                     `json:"name"`
	Containers                []DeploymentContainer      `json:"containers"`
	EndpointBaseURL           string                     `json:"endpoint_base_url"`
	CreatedAt                 time.Time                  `json:"created_at"`
	Compute                   *ContainerCompute          `json:"compute,omitempty"`
	ContainerRegistrySettings *ContainerRegistrySettings `json:"container_registry_settings,omitempty"`
	IsSpot                    bool                       `json:"is_spot"`
}

// TargetNode represents the compute node/GPU configuration
type TargetNode struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

// ContainerRegistrySettings represents registry authentication settings
type ContainerRegistrySettings struct {
	IsPrivate   bool                    `json:"is_private"`
	Credentials *RegistryCredentialsRef `json:"credentials"`
}

// RegistryCredentialsRef references registry credentials by name
type RegistryCredentialsRef struct {
	Name string `json:"name"`
}

// DeploymentContainer represents a container configuration in a deployment response
type DeploymentContainer struct {
	Image               ContainerImage                `json:"image"`
	Name                string                        `json:"name"`
	ExposedPort         int                           `json:"exposed_port"`
	Healthcheck         *ContainerHealthcheck         `json:"healthcheck,omitempty"`
	EntrypointOverrides *ContainerEntrypointOverrides `json:"entrypoint_overrides,omitempty"`
	Env                 []ContainerEnvVar             `json:"env"`
	VolumeMounts        []ContainerVolumeMount        `json:"volume_mounts"`
	AutoUpdate          *ContainerAutoUpdate          `json:"autoupdate,omitempty"`
}

// ContainerImage represents a container image reference
type ContainerImage struct {
	Image         string    `json:"image"`
	LastUpdatedAt time.Time `json:"last_updated_at,omitempty"`
}

// ContainerEnvVar represents an environment variable with type
type ContainerEnvVar struct {
	Type                     string `json:"type"`
	Name                     string `json:"name"`
	ValueOrReferenceToSecret string `json:"value_or_reference_to_secret"`
}

// DeploymentScalingOptions represents scaling configuration for container deployment
type DeploymentScalingOptions struct {
	DeadlineSeconds        int `json:"deadline_seconds,omitempty"`
	MaxReplicaCount        int `json:"max_replica_count"`
	QueueMessageTTLSeconds int `json:"queue_message_ttl_seconds,omitempty"`
}

// ContainerCompute represents compute resources for deployments
type ContainerCompute struct {
	Name string `json:"name"`
	Size int    `json:"size"`
}

// CreateDeploymentRequest represents a request to create a new deployment
type CreateDeploymentRequest struct {
	Name                      string                      `json:"name"`
	IsSpot                    bool                        `json:"is_spot"`
	Compute                   ContainerCompute            `json:"compute"`
	ContainerRegistrySettings ContainerRegistrySettings   `json:"container_registry_settings"`
	Scaling                   ContainerScalingOptions     `json:"scaling"`
	Containers                []CreateDeploymentContainer `json:"containers"`
}

// CreateDeploymentContainer represents a container configuration for create/update requests
type CreateDeploymentContainer struct {
	Name                string                        `json:"name,omitempty"`
	Image               string                        `json:"image"`
	ExposedPort         int                           `json:"exposed_port,omitempty"`
	Healthcheck         *ContainerHealthcheck         `json:"healthcheck,omitempty"`
	EntrypointOverrides *ContainerEntrypointOverrides `json:"entrypoint_overrides,omitempty"`
	Env                 []ContainerEnvVar             `json:"env,omitempty"`
	VolumeMounts        []ContainerVolumeMount        `json:"volume_mounts,omitempty"`
	AutoUpdate          *ContainerAutoUpdate          `json:"autoupdate,omitempty"`
}

// UpdateDeploymentRequest represents a request to update a deployment
type UpdateDeploymentRequest struct {
	IsSpot                    *bool                       `json:"is_spot,omitempty"`
	Compute                   *ContainerCompute           `json:"compute,omitempty"`
	ContainerRegistrySettings *ContainerRegistrySettings  `json:"container_registry_settings,omitempty"`
	Scaling                   *ContainerScalingOptions    `json:"scaling,omitempty"`
	Containers                []CreateDeploymentContainer `json:"containers,omitempty"`
}

// ContainerDeploymentStatus represents the status of a container deployment
type ContainerDeploymentStatus struct {
	Status string `json:"status"`
}

// ContainerScalingOptions represents scaling configuration
type ContainerScalingOptions struct {
	MinReplicaCount              int              `json:"min_replica_count"`
	MaxReplicaCount              int              `json:"max_replica_count"`
	ScaleDownPolicy              *ScalingPolicy   `json:"scale_down_policy"`
	ScaleUpPolicy                *ScalingPolicy   `json:"scale_up_policy"`
	QueueMessageTTLSeconds       int              `json:"queue_message_ttl_seconds"`
	ConcurrentRequestsPerReplica int              `json:"concurrent_requests_per_replica"`
	ScalingTriggers              *ScalingTriggers `json:"scaling_triggers"`
}

// ScalingPolicy represents scale up/down policy configuration
type ScalingPolicy struct {
	DelaySeconds int `json:"delay_seconds"`
}

// ScalingTriggers represents the various scaling triggers
type ScalingTriggers struct {
	QueueLoad      *QueueLoadTrigger   `json:"queue_load"`
	CPUUtilization *UtilizationTrigger `json:"cpu_utilization"`
	GPUUtilization *UtilizationTrigger `json:"gpu_utilization"`
}

// QueueLoadTrigger represents queue load based scaling trigger
type QueueLoadTrigger struct {
	Threshold float64 `json:"threshold"`
}

// UtilizationTrigger represents CPU/GPU utilization based scaling trigger
type UtilizationTrigger struct {
	Enabled   bool `json:"enabled"`
	Threshold int  `json:"threshold"`
}

// UpdateScalingOptionsRequest represents a PATCH request to update scaling options
type UpdateScalingOptionsRequest struct {
	MinReplicaCount              *int             `json:"min_replica_count,omitempty"`
	MaxReplicaCount              *int             `json:"max_replica_count,omitempty"`
	ScaleDownPolicy              *ScalingPolicy   `json:"scale_down_policy,omitempty"`
	ScaleUpPolicy                *ScalingPolicy   `json:"scale_up_policy,omitempty"`
	QueueMessageTTLSeconds       *int             `json:"queue_message_ttl_seconds,omitempty"`
	ConcurrentRequestsPerReplica *int             `json:"concurrent_requests_per_replica,omitempty"`
	ScalingTriggers              *ScalingTriggers `json:"scaling_triggers,omitempty"`
}

// DeploymentReplicas represents replica information for a deployment
type DeploymentReplicas struct {
	List []ReplicaInfo `json:"list"`
}

// ReplicaInfo represents information about a single replica
type ReplicaInfo struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
}

// ContainerEnvVarsRequest represents a request to add/update environment variables
type ContainerEnvVarsRequest struct {
	ContainerName string            `json:"container_name"`
	Env           []ContainerEnvVar `json:"env"`
}

// ContainerHealthcheck represents container healthcheck
type ContainerHealthcheck struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port,omitempty"`
	Path    string `json:"path,omitempty"`
}

// ContainerEntrypointOverrides includes functionality for overriding container entrypoint
type ContainerEntrypointOverrides struct {
	Enabled    bool     `json:"enabled"`
	Entrypoint []string `json:"entrypoint,omitempty"`
	Cmd        []string `json:"cmd,omitempty"`
}

// ContainerVolumeMount represents the container volume mount
type ContainerVolumeMount struct {
	Type       string `json:"type"`
	MountPath  string `json:"mount_path"`
	SecretName string `json:"secret_name,omitempty"`
	SizeInMB   int    `json:"size_in_mb,omitempty"`
	VolumeID   string `json:"volume_id,omitempty"`
}

// ContainerAutoUpdate has automatic update instructions
type ContainerAutoUpdate struct {
	Enabled   bool   `json:"enabled"`
	Mode      string `json:"mode"`
	TagFilter string `json:"tag_filter,omitempty"`
}

// DeleteContainerEnvVarsRequest represents a request to delete environment variables
type DeleteContainerEnvVarsRequest struct {
	ContainerName string   `json:"container_name"`
	Env           []string `json:"env"`
}

// ComputeResource represents available compute resources
type ComputeResource struct {
	Name        string `json:"name"`
	Size        int    `json:"size"`
	IsAvailable bool   `json:"is_available"`
}

// Secret represents a secret used in deployments
type Secret struct {
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	SecretType string    `json:"secret_type"`
}

// CreateSecretRequest represents a request to create a new secret
type CreateSecretRequest struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// FileSecret represents a fileset secret
type FileSecret struct {
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
	SecretType string    `json:"secret_type"`
	FileNames  []string  `json:"file_names"`
}

// CreateFileSecretRequest represents a request to create a fileset secret
type CreateFileSecretRequest struct {
	Name  string           `json:"name"`
	Files []FileSecretFile `json:"files"`
}

// FileSecretFile represents a file in a fileset secret
type FileSecretFile struct {
	Name          string `json:"file_name"`
	Base64Content string `json:"base64_content"`
}

// RegistryCredentials represents container registry credentials
type RegistryCredentials struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// CreateRegistryCredentialsRequest represents a request to create registry credentials
type CreateRegistryCredentialsRequest struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	Username          string `json:"username,omitempty"`
	AccessToken       string `json:"access_token,omitempty"`
	ServiceAccountKey string `json:"service_account_key,omitempty"`
	DockerConfigJson  string `json:"docker_config_json,omitempty"`
	AccessKeyID       string `json:"access_key_id,omitempty"`
	SecretAccessKey   string `json:"secret_access_key,omitempty"`
	Region            string `json:"region,omitempty"`
	EcrRepo           string `json:"ecr_repo,omitempty"`
	ScalewayDomain    string `json:"scaleway_domain,omitempty"`
	ScalewayUUID      string `json:"scaleway_uuid,omitempty"`
}

// Validate validates the ContainerCompute fields
func (r ContainerCompute) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.Size, validation.Required, validation.Min(1)),
	)
}

// Validate validates the CreateDeploymentContainer fields
func (r CreateDeploymentContainer) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Image, validation.Required),
	)
}

// Validate validates the CreateDeploymentRequest fields
func (r CreateDeploymentRequest) Validate() error {
	if err := validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.Compute, validation.Required),
		validation.Field(&r.Containers, validation.Required, validation.Length(1, 0)),
	); err != nil {
		return err
	}
	for i, c := range r.Containers {
		if c.ExposedPort < 1 {
			return fmt.Errorf("containers[%d].exposed_port is required and must be >= 1", i)
		}
	}
	return nil
}

// Validate validates the CreateSecretRequest fields
func (r CreateSecretRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.Value, validation.Required),
	)
}

// Validate validates the CreateFileSecretRequest fields
func (r CreateFileSecretRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.Files, validation.Required, validation.Length(1, 0)),
	)
}

// Validate validates the CreateRegistryCredentialsRequest fields
func (r CreateRegistryCredentialsRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.Type, validation.Required,
			validation.In("verda", "gcr", "dockerhub", "ghcr", "aws-ecr", "scaleway", "custom")),
	)
}

// ValidateCreateDeploymentRequest performs extended validation beyond Validate(),
// including image tag checks and scaling policy requirements.
func ValidateCreateDeploymentRequest(req *CreateDeploymentRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	for _, c := range req.Containers {
		if IsLatestTag(c.Image) {
			return fmt.Errorf("container image %q must use a specific tag, not 'latest'", c.Image)
		}
	}
	if req.Scaling.ScaleDownPolicy == nil {
		return fmt.Errorf("scaling.scale_down_policy is required")
	}
	if req.Scaling.ScaleUpPolicy == nil {
		return fmt.Errorf("scaling.scale_up_policy is required")
	}
	if req.Scaling.ScalingTriggers != nil && req.Scaling.ScalingTriggers.QueueLoad != nil {
		if req.Scaling.ScalingTriggers.QueueLoad.Threshold < 1 {
			return fmt.Errorf("scaling.scaling_triggers.queue_load.threshold must be >= 1")
		}
	}
	return nil
}
