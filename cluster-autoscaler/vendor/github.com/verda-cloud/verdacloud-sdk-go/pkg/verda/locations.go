package verda

import "context"

// Location represents a datacenter location
type Location struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	CountryCode string `json:"country_code"`
}

// Location constants
const (
	LocationFIN01 = "FIN-01"
	LocationFIN03 = "FIN-03"
)

type LocationService struct {
	client *Client
}

func (s *LocationService) Get(ctx context.Context) ([]Location, error) {
	locations, _, err := getRequest[[]Location](ctx, s.client, "/locations")
	if err != nil {
		return nil, err
	}
	return locations, nil
}
