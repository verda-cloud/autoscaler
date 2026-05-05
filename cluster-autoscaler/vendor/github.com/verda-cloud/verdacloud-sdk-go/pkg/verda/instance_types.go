package verda

import (
	"context"
	"fmt"
	"net/url"
)

// InstanceTypeInfo represents detailed instance type information with pricing
type InstanceTypeInfo struct {
	ID                  string          `json:"id"`
	InstanceType        string          `json:"instance_type"`
	Model               string          `json:"model"`
	Name                string          `json:"name"`
	DisplayName         string          `json:"display_name"`
	CPU                 InstanceCPU     `json:"cpu"`
	GPU                 InstanceGPU     `json:"gpu"`
	GPUMemory           InstanceMemory  `json:"gpu_memory"`
	Memory              InstanceMemory  `json:"memory"`
	PricePerHour        FlexibleFloat   `json:"price_per_hour"`
	SpotPrice           FlexibleFloat   `json:"spot_price"`
	DynamicPrice        FlexibleFloat   `json:"dynamic_price"`
	MaxDynamicPrice     FlexibleFloat   `json:"max_dynamic_price"`
	ServerlessPrice     FlexibleFloat   `json:"serverless_price"`
	ServerlessSpotPrice FlexibleFloat   `json:"serverless_spot_price"`
	Storage             InstanceStorage `json:"storage"`
	Currency            string          `json:"currency"`
	Manufacturer        string          `json:"manufacturer"`
	BestFor             []string        `json:"best_for"`
	Description         string          `json:"description"`
	DeployWarning       string          `json:"deploy_warning"`
	P2P                 string          `json:"p2p"`
	SupportedOS         []string        `json:"supported_os"`
}

// PriceHistoryRecord represents a single price record in the price history
type PriceHistoryRecord struct {
	Date                string        `json:"date"`
	FixedPricePerHour   FlexibleFloat `json:"fixed_price_per_hour"`
	DynamicPricePerHour FlexibleFloat `json:"dynamic_price_per_hour"`
	Currency            string        `json:"currency"`
}

// InstanceTypePriceHistory maps instance type names to their price history records
type InstanceTypePriceHistory map[string][]PriceHistoryRecord

type InstanceTypesService struct {
	client *Client
}

func (s *InstanceTypesService) Get(ctx context.Context, currency string) ([]InstanceTypeInfo, error) {
	path := "/instance-types"

	if currency != "" {
		params := url.Values{}
		params.Set("currency", currency)
		path += "?" + params.Encode()
	}

	instanceTypes, _, err := getRequest[[]InstanceTypeInfo](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return instanceTypes, nil
}

func (s *InstanceTypesService) GetByInstanceType(ctx context.Context, instanceType string, isSpot bool, locationCode string, currency string) (*InstanceTypeInfo, error) {
	path := fmt.Sprintf("/instance-types/%s", instanceType)

	params := url.Values{}
	if isSpot {
		params.Set("is_spot", "true")
	}
	if locationCode != "" {
		params.Set("location_code", locationCode)
	}
	if currency != "" {
		params.Set("currency", currency)
	}

	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	instanceTypeInfo, _, err := getRequest[InstanceTypeInfo](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return &instanceTypeInfo, nil
}

// GetPriceHistory returns daily pricing over time (1-12 months)
func (s *InstanceTypesService) GetPriceHistory(ctx context.Context, numOfMonths int, currency string) (InstanceTypePriceHistory, error) {
	path := "/instance-types/price-history"

	params := url.Values{}
	if numOfMonths > 0 {
		params.Set("num_of_months", fmt.Sprintf("%d", numOfMonths))
	}
	if currency != "" {
		params.Set("currency", currency)
	}

	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	priceHistory, _, err := getRequest[InstanceTypePriceHistory](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return priceHistory, nil
}
