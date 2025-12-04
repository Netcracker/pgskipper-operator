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

package monstate

import (
	"github.com/prometheus/client_golang/prometheus"
	"sync"
)

type MetricState struct {
	sync.RWMutex
	m map[string]prometheus.Labels
}

func (ms *MetricState) Initialize() {
	ms.m = make(map[string]prometheus.Labels)
}

func (ms *MetricState) Get(key string) prometheus.Labels {
	ms.RLock()
	defer ms.RUnlock()
	return ms.m[key]
}

func (ms *MetricState) GetAllKeys() []string {
	ms.RLock()
	defer ms.RUnlock()
	result := make([]string, 0, len(ms.m))
	for key := range ms.m {
		result = append(result, key)
	}
	return result
}

func (ms *MetricState) Set(key string, val prometheus.Labels) {
	ms.Lock()
	defer ms.Unlock()
	ms.m[key] = val
}

func (ms *MetricState) Delete(key string) {
	ms.Lock()
	defer ms.Unlock()
	delete(ms.m, key)
}
