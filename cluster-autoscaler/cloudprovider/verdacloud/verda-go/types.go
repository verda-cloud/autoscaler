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

import "fmt"

// Instance statuses reported by the Verda Cloud API.
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

// ActionDelete is the instance action that deletes instances.
const ActionDelete = "delete"

// SpotDiscontinueDeletePermanent deletes a spot instance's volume when the instance is discontinued.
const SpotDiscontinueDeletePermanent = "delete_permanently"

// Instance is a Verda Cloud virtual machine, limited to the fields the autoscaler uses.
type Instance struct {
	ID           string   `json:"id"`
	Status       string   `json:"status"`
	Hostname     string   `json:"hostname"`
	Description  string   `json:"description"`
	Location     string   `json:"location"`
	InstanceType string   `json:"instance_type"`
	IsSpot       bool     `json:"is_spot"`
	OSVolumeID   *string  `json:"os_volume_id"`
	VolumeIDs    []string `json:"volume_ids"`
}

// CreateInstanceRequest is the body of an instance deploy request.
type CreateInstanceRequest struct {
	InstanceType    string                 `json:"instance_type"`
	Image           string                 `json:"image"`
	Hostname        string                 `json:"hostname"`
	Description     string                 `json:"description"`
	SSHKeyIDs       []string               `json:"ssh_key_ids"`
	LocationCode    string                 `json:"location_code,omitempty"`
	Contract        string                 `json:"contract,omitempty"`
	Pricing         string                 `json:"pricing,omitempty"`
	StartupScriptID *string                `json:"startup_script_id,omitempty"`
	Volumes         []VolumeCreateRequest  `json:"volumes,omitempty"`
	OSVolume        *OSVolumeCreateRequest `json:"os_volume,omitempty"`
	IsSpot          bool                   `json:"is_spot,omitempty"`
}

// OSVolumeCreateRequest configures the OS volume of a new instance.
type OSVolumeCreateRequest struct {
	Name              string `json:"name"`
	Size              int    `json:"size"`
	OnSpotDiscontinue string `json:"on_spot_discontinue,omitempty"`
}

// VolumeCreateRequest configures an additional volume created with a new instance.
type VolumeCreateRequest struct {
	Size              int    `json:"size"`
	Type              string `json:"type"`
	Name              string `json:"name"`
	OnSpotDiscontinue string `json:"on_spot_discontinue,omitempty"`
}

// InstanceActionRequest is the body of an instance action, such as delete.
type InstanceActionRequest struct {
	Action            string   `json:"action"`
	ID                []string `json:"id"`
	VolumeIDs         []string `json:"volume_ids"`
	DeletePermanently bool     `json:"delete_permanently,omitempty"`
}

// InstanceActionResult is the per-instance outcome of an action that returned 202 or 207.
type InstanceActionResult struct {
	Action     string `json:"action"`
	InstanceID string `json:"instanceId"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	StatusCode int    `json:"statusCode,omitempty"`
}

// StartupScript is a stored startup script.
type StartupScript struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Script string `json:"script"`
}

// CreateStartupScriptRequest is the body of a startup script create request.
type CreateStartupScriptRequest struct {
	Name   string `json:"name"`
	Script string `json:"script"`
}

// LocationAvailability lists the instance types available in a location.
type LocationAvailability struct {
	LocationCode   string   `json:"location_code"`
	Availabilities []string `json:"availabilities"`
}

// InstanceTypeInfo describes an instance type's resources.
type InstanceTypeInfo struct {
	InstanceType string `json:"instance_type"`
	CPU          struct {
		NumberOfCores int `json:"number_of_cores"`
	} `json:"cpu"`
	GPU struct {
		NumberOfGPUs int `json:"number_of_gpus"`
	} `json:"gpu"`
	Memory struct {
		SizeInGigabytes int `json:"size_in_gigabytes"`
	} `json:"memory"`
}

// APIError is a non-2xx response from the Verda Cloud API.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("verda API error %d (%s): %s", e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("verda API error %d: %s", e.StatusCode, e.Message)
}
