package verda

import (
	"context"
	"fmt"
	"net/url"
)

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
