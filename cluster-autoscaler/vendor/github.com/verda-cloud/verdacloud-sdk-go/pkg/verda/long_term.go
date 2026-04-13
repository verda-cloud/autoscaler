package verda

import (
	"context"
)

type LongTermService struct {
	client *Client
}

func (s *LongTermService) GetInstancePeriods(ctx context.Context) ([]LongTermPeriod, error) {
	path := "/long-term/periods/instances"

	periods, _, err := getRequest[[]LongTermPeriod](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return periods, nil
}

func (s *LongTermService) GetPeriods(ctx context.Context) ([]LongTermPeriod, error) {
	path := "/long-term/periods"

	periods, _, err := getRequest[[]LongTermPeriod](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return periods, nil
}

func (s *LongTermService) GetClusterPeriods(ctx context.Context) ([]LongTermPeriod, error) {
	path := "/long-term/periods/clusters"

	periods, _, err := getRequest[[]LongTermPeriod](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return periods, nil
}
