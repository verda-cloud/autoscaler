package verda

import (
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// ClusterWorkerNode represents a worker node in a cluster
type ClusterWorkerNode struct {
	ID        string `json:"id"`
	Hostname  string `json:"hostname"`
	PublicIP  string `json:"public_ip"`
	PrivateIP string `json:"private_ip"`
	Status    string `json:"status"`
}

// ClusterSharedVolume represents a shared volume attached to a cluster
type ClusterSharedVolume struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	MountPoint      string `json:"mount_point"`
	SizeInGigabytes int    `json:"size_in_gigabytes"`
}

// Cluster represents a Verda cluster
type Cluster struct {
	ID                string                `json:"id"`
	IP                *string               `json:"ip"`
	Status            string                `json:"status"`
	CreatedAt         time.Time             `json:"created_at"`
	CPU               InstanceCPU           `json:"cpu"`
	GPU               InstanceGPU           `json:"gpu"`
	GPUMemory         InstanceMemory        `json:"gpu_memory"`
	Memory            InstanceMemory        `json:"memory"`
	Hostname          string                `json:"hostname"`
	Description       string                `json:"description"`
	Location          string                `json:"location"`
	PricePerHour      FlexibleFloat         `json:"price_per_hour"`
	ClusterType       string                `json:"cluster_type"`
	Image             string                `json:"image"`
	OSName            string                `json:"os_name"`
	SSHKeyIDs         []string              `json:"ssh_key_ids"`
	Contract          string                `json:"contract"`
	StartupScriptID   *string               `json:"startup_script_id,omitempty"`
	AutoRentExtension *bool                 `json:"auto_rent_extension,omitempty"`
	TurnToPayAsYouGo  *bool                 `json:"turn_to_pay_as_you_go,omitempty"`
	ExtensionSettings *string               `json:"extension_settings,omitempty"`
	LongTermPeriod    *string               `json:"long_term_period,omitempty"`
	WorkerNodes       []ClusterWorkerNode   `json:"worker_nodes,omitempty"`
	SharedVolumes     []ClusterSharedVolume `json:"shared_volumes,omitempty"`
}

// ClusterSharedVolumeSpec represents the shared volume specification for cluster creation
type ClusterSharedVolumeSpec struct {
	Name string `json:"name"`
	Size int    `json:"size"` // Size in GB
}

// ClusterExistingVolume represents an existing volume to attach to a cluster
type ClusterExistingVolume struct {
	ID string `json:"id"`
}

// CreateClusterRequest represents the request to create a cluster
type CreateClusterRequest struct {
	ClusterType       string                  `json:"cluster_type"`
	Image             string                  `json:"image"`
	Hostname          string                  `json:"hostname"`
	Description       string                  `json:"description"`
	SSHKeyIDs         []string                `json:"ssh_key_ids"`
	LocationCode      string                  `json:"location_code,omitempty"`
	Contract          string                  `json:"contract,omitempty"`
	StartupScriptID   *string                 `json:"startup_script_id,omitempty"`
	AutoRentExtension *bool                   `json:"auto_rent_extension,omitempty"`
	TurnToPayAsYouGo  *bool                   `json:"turn_to_pay_as_you_go,omitempty"`
	ExtensionSettings *string                 `json:"extension_settings,omitempty"`
	SharedVolume      ClusterSharedVolumeSpec `json:"shared_volume"`
	ExistingVolumes   []ClusterExistingVolume `json:"existing_volumes,omitempty"`
}

// CreateClusterResponse represents the response from creating a cluster
type CreateClusterResponse struct {
	ID string `json:"id"`
}

// ClusterActionItem represents a single cluster action
type ClusterActionItem struct {
	Action string `json:"action"` // Must be "discontinue"
	ID     string `json:"id"`
}

// ClusterActionsRequest represents a request to perform actions on clusters
type ClusterActionsRequest struct {
	Actions []ClusterActionItem `json:"actions"`
}

// ClusterAvailability represents cluster availability information by location
type ClusterAvailability struct {
	LocationCode   string   `json:"location_code"`
	Availabilities []string `json:"availabilities"`
}

// ClusterType represents a cluster configuration type
type ClusterType struct {
	ID           string         `json:"id"`
	Model        string         `json:"model"`
	Name         string         `json:"name"`
	ClusterType  string         `json:"cluster_type"`
	CPU          InstanceCPU    `json:"cpu"`
	GPU          InstanceGPU    `json:"gpu"`
	GPUMemory    InstanceMemory `json:"gpu_memory"`
	Memory       InstanceMemory `json:"memory"`
	PricePerHour FlexibleFloat  `json:"price_per_hour"`
	Currency     string         `json:"currency"`
	Manufacturer string         `json:"manufacturer"`
	NodeDetails  []interface{}  `json:"node_details"`
	SupportedOS  []string       `json:"supported_os"`
}

// ClusterImage represents an OS image for clusters
type ClusterImage struct {
	ID        string   `json:"id"`
	ImageType string   `json:"image_type"`
	Name      string   `json:"name"`
	IsDefault bool     `json:"is_default"`
	Details   []string `json:"details"`
	Category  string   `json:"category"`
	IsCluster bool     `json:"is_cluster"`
}

// Cluster action constants
const (
	ClusterActionDiscontinue = "discontinue"
)

// Cluster extension settings constants
const (
	ExtensionSettingsAutoRenew   = "auto_renew"
	ExtensionSettingsPayAsYouGo  = "pay_as_you_go"
	ExtensionSettingsEndContract = "end_contract"
)

// Validate validates the CreateClusterRequest fields
func (r CreateClusterRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.ClusterType, validation.Required),
		validation.Field(&r.Image, validation.Required),
		validation.Field(&r.Hostname, validation.Required),
		validation.Field(&r.Description, validation.Required),
		validation.Field(&r.SharedVolume, validation.Required),
		validation.Field(&r.Contract,
			validation.In("PAY_AS_YOU_GO", "LONG_TERM")),
	)
}

// Validate validates the ClusterSharedVolumeSpec fields
func (r ClusterSharedVolumeSpec) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.Size, validation.Required, validation.Min(1)),
	)
}
