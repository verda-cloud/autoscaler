package verda

import "context"

type ImagesService struct {
	client *Client
}

func (s *ImagesService) Get(ctx context.Context) ([]Image, error) {
	images, _, err := getRequest[[]Image](ctx, s.client, "/images")
	if err != nil {
		return nil, err
	}
	return images, nil
}

func (s *ImagesService) GetClusterImages(ctx context.Context) ([]ClusterImage, error) {
	images, _, err := getRequest[[]ClusterImage](ctx, s.client, "/images/cluster")
	if err != nil {
		return nil, err
	}
	return images, nil
}
