package verda

import (
	"context"
	"fmt"
	"net/url"
)

type InstanceAvailabilityService struct {
	client *Client
}

func (s *InstanceAvailabilityService) GetAllAvailabilities(ctx context.Context, isSpot bool, locationCode string) ([]LocationAvailability, error) {
	path := "/instance-availability"

	params := url.Values{}
	if isSpot {
		params.Set("is_spot", "true")
	}
	if locationCode != "" {
		params.Set("location_code", locationCode)
	}

	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	availabilities, _, err := getRequest[[]LocationAvailability](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return availabilities, nil
}

func (s *InstanceAvailabilityService) GetInstanceTypeAvailability(ctx context.Context, instanceType string, isSpot bool, locationCode string) (bool, error) {
	path := fmt.Sprintf("/instance-availability/%s", instanceType)

	params := url.Values{}
	if isSpot {
		params.Set("is_spot", "true")
	}
	if locationCode != "" {
		params.Set("location_code", locationCode)
	}

	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	available, _, err := getRequest[bool](ctx, s.client, path)
	if err != nil {
		return false, err
	}

	return available, nil
}
