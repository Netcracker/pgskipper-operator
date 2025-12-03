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

package metrics

import "github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"

type Row map[string]interface{}

type SortedMap struct {
	keys         []string
	mappedValues map[string]interface{}
}

type SortedMapIterator struct {
	pos int
	sm  SortedMap
}

func (sm *SortedMap) Put(key string, value interface{}) {
	if sm.keys == nil {
		sm.keys = make([]string, 0)
	}
	if sm.mappedValues == nil {
		sm.mappedValues = make(map[string]interface{})
	}
	if !util.Contains(sm.keys, key) {
		sm.keys = append(sm.keys, key)

	}
	sm.mappedValues[key] = value
}

func (sm *SortedMap) Iterator() SortedMapIterator {
	return SortedMapIterator{sm: *sm}
}

func (smi *SortedMapIterator) Next() bool {
	result := smi.pos < len(smi.sm.keys)
	if result {
		smi.pos = smi.pos + 1
	}
	return result
}

func (smi *SortedMapIterator) GetPair() (string, interface{}) {
	index := smi.pos - 1
	if index < 0 {
		return "", nil
	}
	key := smi.sm.keys[smi.pos-1]
	value := smi.sm.mappedValues[key]
	return key, value
}
