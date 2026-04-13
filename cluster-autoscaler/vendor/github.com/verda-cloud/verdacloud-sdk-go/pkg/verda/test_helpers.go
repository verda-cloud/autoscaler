package verda

import "github.com/verda-cloud/verdacloud-sdk-go/pkg/verda/testutil"

func NewTestClient(mockServer *testutil.MockServer) *Client {
	config := testutil.NewTestClientConfig(mockServer)
	client, _ := NewClient(
		WithBaseURL(config.BaseURL),
		WithClientID(config.ClientID),
		WithClientSecret(config.ClientSecret),
	)
	return client
}

func NewTestClientWithUserAgent(mockServer *testutil.MockServer, userAgent string) *Client {
	config := testutil.NewTestClientConfig(mockServer)
	client, _ := NewClient(
		WithBaseURL(config.BaseURL),
		WithClientID(config.ClientID),
		WithClientSecret(config.ClientSecret),
		WithUserAgent(userAgent),
	)
	return client
}
