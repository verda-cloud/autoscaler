package verda

import (
	"time"

	validation "github.com/go-ozzo/ozzo-validation/v4"
)

// VolumeCreateRequest represents a volume to be created
type VolumeCreateRequest struct {
	Size              int      `json:"size"`
	Type              string   `json:"type"`
	Name              string   `json:"name"`
	LocationCode      string   `json:"location_code,omitempty"`
	OnSpotDiscontinue string   `json:"on_spot_discontinue,omitempty"`
	InstanceID        string   `json:"instance_id,omitempty"`
	InstanceIDs       []string `json:"instance_ids,omitempty"`
}

// VolumeAttachRequest represents a request to attach a volume to an instance
type VolumeAttachRequest struct {
	InstanceID string `json:"instance_id"`
}

// VolumeDetachRequest represents a request to detach a volume from an instance
type VolumeDetachRequest struct {
	InstanceID string `json:"instance_id"`
}

// VolumeActionRequest represents an action to perform on volumes
type VolumeActionRequest struct {
	ID           string   `json:"id"`
	Action       string   `json:"action"`
	Name         string   `json:"name,omitempty"`
	Type         string   `json:"type,omitempty"`
	Size         int      `json:"size,omitempty"`
	InstanceID   string   `json:"instance_id,omitempty"`
	InstanceIDs  []string `json:"instance_ids,omitempty"`
	IsPermanent  bool     `json:"is_permanent,omitempty"`
	LocationCode string   `json:"location_code,omitempty"`
}

// VolumeCloneRequest represents a request to clone a volume
type VolumeCloneRequest struct {
	Name         string `json:"name"`
	LocationCode string `json:"location_code,omitempty"`
}

// VolumeResizeRequest represents a request to resize a volume
type VolumeResizeRequest struct {
	Size int `json:"size"`
}

// VolumeRenameRequest represents a request to rename a volume
type VolumeRenameRequest struct {
	Name string `json:"name"`
}

// VolumeAttachedInstance represents a summary of an instance that has a volume attached
type VolumeAttachedInstance struct {
	ID                  string  `json:"id"`
	AutoRentalExtension *bool   `json:"auto_rental_extension"`
	IP                  *string `json:"ip"`
	InstanceType        string  `json:"instance_type"`
	Status              string  `json:"status"`
	OSVolumeID          string  `json:"os_volume_id"`
	Hostname            string  `json:"hostname"`
}

// VolumeLongTerm represents long-term rental details for a volume
type VolumeLongTerm struct {
	EndDate             time.Time `json:"end_date"`
	LongTermPeriod      string    `json:"long_term_period"`
	DiscountPercentage  int       `json:"discount_percentage"`
	AutoRentalExtension bool      `json:"auto_rental_extension"`
	NextPeriodPrice     float64   `json:"next_period_price"`
	CurrentPeriodPrice  float64   `json:"current_period_price"`
}

// Volume represents a Verda volume
type Volume struct {
	ID                       string                   `json:"id"`
	Name                     string                   `json:"name"`
	Size                     int                      `json:"size"`
	Type                     string                   `json:"type"`
	Status                   string                   `json:"status"`
	CreatedAt                time.Time                `json:"created_at"`
	InstanceID               *string                  `json:"instance_id"`
	Instances                []VolumeAttachedInstance `json:"instances"`
	Location                 string                   `json:"location"`
	Contract                 string                   `json:"contract,omitempty"`
	IsOSVolume               bool                     `json:"is_os_volume"`
	Target                   *string                  `json:"target"`
	SSHKeyIDs                []string                 `json:"ssh_key_ids"`
	PseudoPath               *string                  `json:"pseudo_path"`
	CreateDirectoryCommand   *string                  `json:"create_directory_command"`
	MountCommand             *string                  `json:"mount_command"`
	FilesystemToFstabCommand *string                  `json:"filesystem_to_fstab_command"`
	BaseHourlyCost           float64                  `json:"base_hourly_cost"`
	MonthlyPrice             float64                  `json:"monthly_price"`
	Currency                 string                   `json:"currency"`
	LongTerm                 *VolumeLongTerm          `json:"long_term"`
}

// VolumeInTrash represents a volume that has been moved to trash
type VolumeInTrash struct {
	ID                   string                   `json:"id"`
	Name                 string                   `json:"name"`
	Size                 int                      `json:"size"`
	Type                 string                   `json:"type"`
	Status               string                   `json:"status"`
	CreatedAt            time.Time                `json:"created_at"`
	DeletedAt            time.Time                `json:"deleted_at"`
	InstanceID           *string                  `json:"instance_id"`
	Instances            []VolumeAttachedInstance `json:"instances"`
	Location             string                   `json:"location"`
	Contract             string                   `json:"contract"`
	IsOSVolume           bool                     `json:"is_os_volume"`
	Target               *string                  `json:"target"`
	SSHKeyIDs            []string                 `json:"ssh_key_ids"`
	BaseHourlyCost       float64                  `json:"base_hourly_cost"`
	MonthlyPrice         float64                  `json:"monthly_price"`
	Currency             string                   `json:"currency"`
	IsPermanentlyDeleted bool                     `json:"is_permanently_deleted"`
}

// Volume type constants
const (
	VolumeTypeHDD               = "HDD"
	VolumeTypeNVMe              = "NVMe"
	VolumeTypeHDDShared         = "HDD_Shared"
	VolumeTypeNVMeShared        = "NVMe_Shared"
	VolumeTypeNVMeLocalStorage  = "NVMe_Local_Storage"
	VolumeTypeNVMeSharedCluster = "NVMe_Shared_Cluster"
	VolumeTypeNVMeOSCluster     = "NVMe_OS_Cluster"
)

// Volume status constants - these match the actual API values
const (
	VolumeStatusOrdered   = "ordered"
	VolumeStatusAttached  = "attached"
	VolumeStatusAttaching = "attaching"
	VolumeStatusDetached  = "detached"
	VolumeStatusDeleted   = "deleted"
	VolumeStatusCloning   = "cloning"
	VolumeStatusDetaching = "detaching"
	VolumeStatusDeleting  = "deleting"
	VolumeStatusRestoring = "restoring"
	VolumeStatusCreated   = "created"
	VolumeStatusExported  = "exported"
	VolumeStatusCanceled  = "canceled"
	VolumeStatusCanceling = "canceling"
)

// Volume action constants
const (
	VolumeActionAttach = "attach"
	VolumeActionDetach = "detach"
	VolumeActionRename = "rename"
	VolumeActionResize = "resize"
	VolumeActionDelete = "delete"
	VolumeActionClone  = "clone"
)

// Validate validates the VolumeCreateRequest fields
func (r VolumeCreateRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
		validation.Field(&r.Size, validation.Required, validation.Min(1)),
		validation.Field(&r.Type, validation.Required,
			validation.In(VolumeTypeHDD, VolumeTypeNVMe, VolumeTypeHDDShared,
				VolumeTypeNVMeShared, VolumeTypeNVMeLocalStorage,
				VolumeTypeNVMeSharedCluster, VolumeTypeNVMeOSCluster)),
	)
}

// Validate validates the VolumeCloneRequest fields
func (r VolumeCloneRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
	)
}

// Validate validates the VolumeResizeRequest fields
func (r VolumeResizeRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Size, validation.Required, validation.Min(1)),
	)
}

// Validate validates the VolumeRenameRequest fields
func (r VolumeRenameRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required),
	)
}

// Validate validates the VolumeAttachRequest fields
func (r VolumeAttachRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.InstanceID, validation.Required),
	)
}

// Validate validates the VolumeDetachRequest fields
func (r VolumeDetachRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.InstanceID, validation.Required),
	)
}
