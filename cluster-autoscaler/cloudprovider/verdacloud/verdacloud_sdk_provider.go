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
	"os"
	"time"

	"github.com/verda-cloud/verdacloud-sdk-go/pkg/verda"
	klog "k8s.io/klog/v2"
)

const (
	// Identifies the autoscaler in API requests.
	autoscalerUserAgent = "cluster-autoscaler/verdacloud"

	// Retry budget for transient SDK failures.
	sdkMaxRetries     = 3
	sdkInitialBackoff = time.Second
)

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

	client.AddRequestMiddleware(
		verda.ExponentialBackoffRetryMiddleware(sdkMaxRetries, sdkInitialBackoff, logger),
	)

	if detailedDebugEnabled {
		verda.AddDetailedDebugLogging(client)
		klog.V(4).Info("VerdaCloud SDK detailed debug logging enabled (VERDA_DEBUG=true)")
	}

	klog.V(4).Infof("VerdaCloud SDK client created with retry middleware enabled (max %d retries, exponential backoff from %s)", sdkMaxRetries, sdkInitialBackoff)

	return &verdacloudSDKProvider{
		client: client,
	}, nil
}
