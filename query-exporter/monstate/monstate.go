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
	"sync"
)

type MonitoringState struct {
	sync.RWMutex
	m map[string]*MetricState
}

func (ms *MonitoringState) Initialize() {
	ms.m = make(map[string]*MetricState)
}

func (ms *MonitoringState) Get(key string) *MetricState {
	ms.RLock()
	defer ms.RUnlock()
	return ms.m[key]
}

func (ms *MonitoringState) Set(key string, val *MetricState) {
	ms.Lock()
	defer ms.Unlock()
	ms.m[key] = val
}
