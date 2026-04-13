package verda

import (
	"context"
	"net/url"
)

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
