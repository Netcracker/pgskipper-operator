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

package utils

import (
	"sync"
)

type SyncStringSet struct {
	sync.RWMutex
	m map[string]bool
}

func (set *SyncStringSet) Initialize() {
	set.m = make(map[string]bool)
}

func (set *SyncStringSet) Get(key string) bool {
	set.RLock()
	defer set.RUnlock()
	return set.m[key]
}

func (set *SyncStringSet) Add(key string) {
	set.Lock()
	defer set.Unlock()
	set.m[key] = true
}

func (set *SyncStringSet) Remove(key string) {
	set.Lock()
	defer set.Unlock()
	delete(set.m, key)
}

func (set *SyncStringSet) GetAllEntries() []string {
	set.RLock()
	defer set.RUnlock()
	result := make([]string, 0, len(set.m))
	for key := range set.m {
		result = append(result, key)
	}
	return result
}
