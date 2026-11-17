/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package verdacloud

import (
	"fmt"
	"net/http"
	"os"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	klog "k8s.io/klog/v2"
)

// Identifies the autoscaler in API requests.
const autoscalerUserAgent = "cluster-autoscaler/verdacloud"

type verdacloudSDKProvider struct {
	client *verda.Client
}

func createVerdacloudSDKProvider(cfg *cloudConfig) (*verdacloudSDKProvider, error) {
	clientID := os.Getenv("VERDA_CLIENT_ID")
	clientSecret := os.Getenv("VERDA_CLIENT_SECRET")
	baseURL := os.Getenv("VERDA_BASE_URL")

	if clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("VERDA_CLIENT_ID and VERDA_CLIENT_SECRET environment variables must be set")
	}

	verdaDebug := os.Getenv("VERDA_DEBUG")
	detailedDebugEnabled := verdaDebug == "true" || verdaDebug == "1"

	var logger verda.Logger
	if cfg.Debug || detailedDebugEnabled {
		logger = verda.NewStdLogger(true)
	} else {
		logger = &verda.NoOpLogger{}
	}

	clientOpts := []verda.ClientOption{
		verda.WithClientID(clientID),
		verda.WithClientSecret(clientSecret),
		verda.WithDebugLogging(cfg.Debug),
		verda.WithLogger(logger),
		verda.WithUserAgent(autoscalerUserAgent),
		// Backstop for calls whose context carries no deadline; withAPIDeadline is the primary bound.
		verda.WithHTTPClient(&http.Client{Timeout: apiCallTimeout}),
	}

	if baseURL != "" {
		clientOpts = append(clientOpts, verda.WithBaseURL(baseURL))
		klog.V(4).Infof("Using VerdaCloud API base URL from VERDA_BASE_URL: %s", baseURL)
	} else {
		klog.V(4).Infof("Using default VerdaCloud API base URL: %s", verda.DefaultBaseURL)
	}

	client, err := verda.NewClient(clientOpts...)
	if err != nil {
		return nil, err
	}

	// No SDK retry middleware: request middleware runs before HTTPClient.Do, so it cannot retry
	// the HTTP call. Failed calls surface to the autoscaler loop, which retries on its next pass.
	if detailedDebugEnabled {
		verda.AddDetailedDebugLogging(client)
		klog.V(4).Info("VerdaCloud SDK detailed debug logging enabled (VERDA_DEBUG=true)")
	}

	return &verdacloudSDKProvider{
		client: client,
	}, nil
}
