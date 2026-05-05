package verda

import (
	"context"
	"encoding/json"
)

// VolumeType represents available volume type specifications
type VolumeType struct {
	Type                 string          `json:"type"`
	Price                VolumeTypePrice `json:"price"`
	IsSharedFS           bool            `json:"is_shared_fs"`
	BurstBandwidth       float64         `json:"burst_bandwidth"`
	ContinuousBandwidth  float64         `json:"continuous_bandwidth"`
	InternalNetworkSpeed float64         `json:"internal_network_speed"`
	ThroughputGbps       float64         `json:"throughput_gbps"`
	IOPS                 string          `json:"iops"`
}

// VolumeTypePrice represents the pricing structure for a volume type
type VolumeTypePrice struct {
	// PricePerMonthPerGB matches the current API response field `price_per_month_per_gb`.
	// New code should use this field.
	PricePerMonthPerGB float64 `json:"price_per_month_per_gb"`
	CPSPerGB           float64 `json:"cps_per_gb"`
	// Deprecated: use PricePerMonthPerGB. MonthlyPerGB is a compatibility alias for older SDK callers and is not part of the current API payload.
	MonthlyPerGB float64 `json:"-"`
	Currency     string  `json:"currency"`
}

// UnmarshalJSON accepts both the current API field `price_per_month_per_gb`
// and the legacy field `monthly_per_gb`, but new code should read PricePerMonthPerGB.
func (p *VolumeTypePrice) UnmarshalJSON(data []byte) error {
	type priceAlias struct {
		PricePerMonthPerGB *float64 `json:"price_per_month_per_gb"`
		MonthlyPerGB       *float64 `json:"monthly_per_gb"`
		CPSPerGB           float64  `json:"cps_per_gb"`
		Currency           string   `json:"currency"`
	}

	var raw priceAlias
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	p.CPSPerGB = raw.CPSPerGB
	p.Currency = raw.Currency

	switch {
	case raw.PricePerMonthPerGB != nil:
		p.PricePerMonthPerGB = *raw.PricePerMonthPerGB
	case raw.MonthlyPerGB != nil:
		p.PricePerMonthPerGB = *raw.MonthlyPerGB
	default:
		p.PricePerMonthPerGB = 0
	}

	p.MonthlyPerGB = p.PricePerMonthPerGB
	return nil
}

type VolumeTypeService struct {
	client *Client
}

func (s *VolumeTypeService) GetAllVolumeTypes(ctx context.Context) ([]VolumeType, error) {
	volumeTypes, _, err := getRequest[[]VolumeType](ctx, s.client, "/volume-types")
	if err != nil {
		return nil, err
	}
	return volumeTypes, nil
}
