package verda

import (
	"context"
	"net/url"
)

// ContainerType represents a serverless container compute resource option
type ContainerType struct {
	ID                  string         `json:"id"`
	Model               string         `json:"model"`
	Name                string         `json:"name"`
	InstanceType        string         `json:"instance_type"`
	CPU                 InstanceCPU    `json:"cpu"`
	GPU                 InstanceGPU    `json:"gpu"`
	GPUMemory           InstanceMemory `json:"gpu_memory"`
	Memory              InstanceMemory `json:"memory"`
	ServerlessPrice     FlexibleFloat  `json:"serverless_price"`
	ServerlessSpotPrice FlexibleFloat  `json:"serverless_spot_price"`
	Currency            string         `json:"currency"`
	Manufacturer        string         `json:"manufacturer"`
}

type ContainerTypesService struct {
	client *Client
}

func (s *ContainerTypesService) Get(ctx context.Context, currency string) ([]ContainerType, error) {
	path := "/container-types"

	if currency != "" {
		params := url.Values{}
		params.Set("currency", currency)
		path += "?" + params.Encode()
	}

	containerTypes, _, err := getRequest[[]ContainerType](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return containerTypes, nil
}
