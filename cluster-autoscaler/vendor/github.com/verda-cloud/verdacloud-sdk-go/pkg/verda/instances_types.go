package verda

import (
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// InstanceCPU represents CPU information
type InstanceCPU struct {
	Description   string `json:"description"`
	NumberOfCores int    `json:"number_of_cores"`
}

// InstanceGPU represents GPU information
type InstanceGPU struct {
	Description  string `json:"description"`
	NumberOfGPUs int    `json:"number_of_gpus"`
}

// InstanceMemory represents memory information
type InstanceMemory struct {
	Description     string `json:"description"`
	SizeInGigabytes int    `json:"size_in_gigabytes"`
}

// InstanceStorage represents storage information
type InstanceStorage struct {
	Description string `json:"description"`
}

// Instance represents a Verda instance
type Instance struct {
	ID              string          `json:"id"`
	IP              *string         `json:"ip"`
	Status          string          `json:"status"`
	CreatedAt       time.Time       `json:"created_at"`
	CPU             InstanceCPU     `json:"cpu"`
	GPU             InstanceGPU     `json:"gpu"`
	GPUMemory       InstanceMemory  `json:"gpu_memory"`
	Memory          InstanceMemory  `json:"memory"`
	Storage         InstanceStorage `json:"storage"`
	Hostname        string          `json:"hostname"`
	Description     string          `json:"description"`
	Location        string          `json:"location"`
	PricePerHour    FlexibleFloat   `json:"price_per_hour"`
	IsSpot          bool            `json:"is_spot"`
	InstanceType    string          `json:"instance_type"`
	Image           string          `json:"image"`
	OSName          string          `json:"os_name"`
	StartupScriptID *string         `json:"startup_script_id"`
	SSHKeyIDs       []string        `json:"ssh_key_ids"`
	OSVolumeID      *string         `json:"os_volume_id"`
	JupyterToken    string          `json:"jupyter_token"`
	Contract        string          `json:"contract"`
	Pricing         string          `json:"pricing"`
	VolumeIDs       []string        `json:"volume_ids"`
}

// CreateInstanceRequest represents the request to create an instance
type CreateInstanceRequest struct {
	InstanceType    string                 `json:"instance_type"`
	Image           string                 `json:"image"`
	Hostname        string                 `json:"hostname"`
	Description     string                 `json:"description"`
	SSHKeyIDs       []string               `json:"ssh_key_ids,omitempty"`
	LocationCode    string                 `json:"location_code,omitempty"`
	Contract        string                 `json:"contract,omitempty"`
	Pricing         string                 `json:"pricing,omitempty"`
	StartupScriptID *string                `json:"startup_script_id,omitempty"`
	Volumes         []VolumeCreateRequest  `json:"volumes,omitempty"`
	ExistingVolumes []string               `json:"existing_volumes,omitempty"`
	OSVolume        *OSVolumeCreateRequest `json:"os_volume,omitempty"`
	IsSpot          bool                   `json:"is_spot,omitempty"`
	Coupon          *string                `json:"coupon,omitempty"`
}

// OSVolumeCreateRequest represents OS volume configuration
type OSVolumeCreateRequest struct {
	Name              string `json:"name"`
	Size              int    `json:"size"`
	OnSpotDiscontinue string `json:"on_spot_discontinue,omitempty"`
}

// InstanceActionRequest represents an action to perform on instances.
// VolumeIDs: nil omits the field (API default: OS volume deleted, rest detached),
// empty slice sends [] (no volumes deleted), slice with IDs deletes those volumes.
type InstanceActionRequest struct {
	Action            string   `json:"action"`
	ID                []string `json:"id"`
	VolumeIDs         []string `json:"volume_ids"`
	DeletePermanently bool     `json:"delete_permanently,omitempty"`
}

// InstanceActionResult represents the per-instance outcome of an action request.
// Returned as an array for 202 (all succeeded) and 207 (some failed) responses.
type InstanceActionResult struct {
	Action     string `json:"action"`
	InstanceID string `json:"instanceId"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	StatusCode int    `json:"statusCode,omitempty"`
}

// Action constants
const (
	ActionBoot          = "boot"
	ActionStart         = "start"
	ActionShutdown      = "shutdown"
	ActionDelete        = "delete"
	ActionDiscontinue   = "discontinue"
	ActionHibernate     = "hibernate"
	ActionConfigureSpot = "configure_spot"
	ActionForceShutdown = "force_shutdown"
	ActionDeleteStuck   = "delete_stuck"
	ActionDeploy        = "deploy"
	ActionTransfer      = "transfer"
)

// Instance status constants
const (
	StatusNew          = "new"
	StatusOrdered      = "ordered"
	StatusProvisioning = "provisioning"
	StatusValidating   = "validating"
	StatusRunning      = "running"
	StatusOffline      = "offline"
	StatusPending      = "pending"
	StatusDiscontinued = "discontinued"
	StatusUnknown      = "unknown"
	StatusNotFound     = "notfound"
	StatusError        = "error"
	StatusDeleting     = "deleting"
	StatusNoCapacity   = "no_capacity"
)

// Spot discontinue policy constants for volume behavior when a spot instance is discontinued
const (
	SpotDiscontinueKeepDetached    = "keep_detached"
	SpotDiscontinueMoveToTrash     = "move_to_trash"
	SpotDiscontinueDeletePermanent = "delete_permanently"
)

// Validate validates the CreateInstanceRequest fields
func (r CreateInstanceRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.InstanceType, validation.Required),
		validation.Field(&r.Image, validation.Required),
		validation.Field(&r.Hostname, validation.Required),
		validation.Field(&r.Description, validation.Required),
		validation.Field(&r.Contract,
			validation.In("LONG_TERM", "PAY_AS_YOU_GO", "SPOT")),
		validation.Field(&r.OSVolume),
		validation.Field(&r.Volumes),
	)
}

// Validate validates the OSVolumeCreateRequest fields
func (r OSVolumeCreateRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.Size, validation.Required, validation.Min(1)),
		validation.Field(&r.OnSpotDiscontinue,
			validation.In("keep_detached", "move_to_trash", "delete_permanently")),
	)
}

// Validate validates the InstanceActionRequest fields
func (r InstanceActionRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Action, validation.Required,
			validation.In(ActionBoot, ActionStart, ActionShutdown, ActionDelete,
				ActionDiscontinue, ActionHibernate, ActionConfigureSpot,
				ActionForceShutdown, ActionDeleteStuck, ActionDeploy, ActionTransfer)),
		validation.Field(&r.ID, validation.Required, validation.Length(1, 0)),
	)
}
