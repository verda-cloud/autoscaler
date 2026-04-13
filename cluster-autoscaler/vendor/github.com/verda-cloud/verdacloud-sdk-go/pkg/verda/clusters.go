package verda

import (
	"context"
	"fmt"
	"net/url"
)

type ClusterService struct {
	client *Client
}

func (s *ClusterService) Get(ctx context.Context) ([]Cluster, error) {
	clusters, _, err := getRequest[[]Cluster](ctx, s.client, "/clusters")
	if err != nil {
		return nil, err
	}
	return clusters, nil
}

func (s *ClusterService) GetByID(ctx context.Context, id string) (*Cluster, error) {
	path := fmt.Sprintf("/clusters/%s", id)

	cluster, _, err := getRequest[Cluster](ctx, s.client, path)
	if err != nil {
		return nil, err
	}
	return &cluster, nil
}

func (s *ClusterService) Create(ctx context.Context, req CreateClusterRequest) (*CreateClusterResponse, error) {
	if req.LocationCode == "" {
		req.LocationCode = LocationFIN01
	}

	response, _, err := postRequest[CreateClusterResponse](ctx, s.client, "/clusters", req)
	if err != nil {
		return nil, err
	}

	return &response, nil
}

// Discontinue discontinues one or more clusters
// Note: Only discontinue action is allowed for clusters.
// Important: Local OS storage will be deleted. Shared volumes will be detached and should be deleted manually.
func (s *ClusterService) Discontinue(ctx context.Context, clusterIDs []string) error {
	actions := make([]ClusterActionItem, len(clusterIDs))
	for i, id := range clusterIDs {
		actions[i] = ClusterActionItem{
			Action: ClusterActionDiscontinue,
			ID:     id,
		}
	}

	req := ClusterActionsRequest{
		Actions: actions,
	}

	_, _, err := putRequest[any](ctx, s.client, "/clusters", req)
	return err
}

func (s *ClusterService) GetClusterTypes(ctx context.Context, currency string) ([]ClusterType, error) {
	path := "/cluster-types"

	if currency != "" {
		params := url.Values{}
		params.Set("currency", currency)
		path += "?" + params.Encode()
	}

	clusterTypes, _, err := getRequest[[]ClusterType](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return clusterTypes, nil
}

func (s *ClusterService) GetAvailabilities(ctx context.Context, locationCode string) ([]ClusterAvailability, error) {
	path := "/cluster-availability"

	if locationCode != "" {
		params := url.Values{}
		params.Set("location_code", locationCode)
		path += "?" + params.Encode()
	}

	availabilities, _, err := getRequest[[]ClusterAvailability](ctx, s.client, path)
	if err != nil {
		return nil, err
	}

	return availabilities, nil
}

func (s *ClusterService) CheckClusterTypeAvailability(ctx context.Context, clusterType string, locationCode string) (bool, error) {
	path := fmt.Sprintf("/cluster-availability/%s", clusterType)

	if locationCode != "" {
		params := url.Values{}
		params.Set("location_code", locationCode)
		path += "?" + params.Encode()
	}

	available, _, err := getRequest[bool](ctx, s.client, path)
	if err != nil {
		return false, err
	}

	return available, nil
}

func (s *ClusterService) GetImages(ctx context.Context) ([]ClusterImage, error) {
	images, _, err := getRequest[[]ClusterImage](ctx, s.client, "/images/cluster")
	if err != nil {
		return nil, err
	}

	return images, nil
}
