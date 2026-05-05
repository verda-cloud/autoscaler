package verda

import "context"

// Balance represents account balance information
type Balance struct {
	Amount   float64 `json:"amount"`
	Currency string  `json:"currency"`
}

type BalanceService struct {
	client *Client
}

func (s *BalanceService) Get(ctx context.Context) (*Balance, error) {
	balance, _, err := getRequest[Balance](ctx, s.client, "/balance")
	if err != nil {
		return nil, err
	}
	return &balance, nil
}
