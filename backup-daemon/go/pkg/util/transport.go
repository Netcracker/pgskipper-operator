// Copyright 2024-2025 NetCracker Technology Corporation
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"fmt"
	"net/http"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"
)

const retryNumEnv = "K8S_CLIENT_RETRY_NUM"

type Retry struct {
	nums int
	http.RoundTripper
}

func (r *Retry) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	logger := Logger
	logger.Debug(fmt.Sprintf("executing request: %s with retries", req.URL))
	for i := 0; i < r.nums; i++ {
		logger.Debug(fmt.Sprintf("attempt: %v of %v", i+1, r.nums))
		resp, err = r.RoundTripper.RoundTrip(req)
		if err != nil && resp == nil {
			logger.Error(fmt.Sprintf("received not retryable error %v", err))
			return
		} else if err != nil || resp.StatusCode >= 500 {
			logger.Warn(fmt.Sprintf("received retryable error %v, with status code: %d ,retrying...", err, resp.StatusCode))
			time.Sleep(10 * time.Second)
			continue
		} else {
			logger.Debug("executed successfully, exiting")
			return
		}
	}
	logger.Warn("no more retries, giving up...")
	return
}

func UpdateTransport(cfg *rest.Config) {
	tc, err := cfg.TransportConfig()
	if err != nil {
		panic(err)
	}
	rt, err := transport.New(tc)
	if err != nil {
		panic(err)
	}
	cfg.Transport = getRetryTransport(rt)

	// Security moved to transport level
	cfg.TLSClientConfig = rest.TLSClientConfig{}
}

func getRetryTransport(rt http.RoundTripper) *Retry {
	retryNum := getEnvInt(retryNumEnv, 10)
	return &Retry{
		nums:         retryNum,
		RoundTripper: rt,
	}
}
