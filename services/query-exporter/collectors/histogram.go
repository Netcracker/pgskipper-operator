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

package collectors

import (
	"fmt"
	"strings"
	"sync"

	"github.com/Netcracker/qubership-query-exporter/utils"

	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
)

type CustomHistogram struct {
	sync.RWMutex
	constHistogramMap map[string]*prometheus.Metric
	stateMap          map[string]*CurrentHistogramState
	Desc              *prometheus.Desc
	LabelsMap         map[string][]string
	summarize         bool
}

type CurrentHistogramState struct {
	Sum     float64
	Cnt     uint64
	Buckets map[float64]uint64
}

func NewCustomHistogram(desc *prometheus.Desc, summarize bool) *CustomHistogram {
	customHistogram := CustomHistogram{}
	customHistogram.Desc = desc
	customHistogram.LabelsMap = make(map[string][]string)
	customHistogram.stateMap = make(map[string]*CurrentHistogramState)
	customHistogram.constHistogramMap = make(map[string]*prometheus.Metric)
	customHistogram.summarize = summarize
	return &customHistogram
}

func (e *CustomHistogram) Describe(ch chan<- *prometheus.Desc) {
	e.RLock()
	defer e.RUnlock()
	for _, constHistogram := range e.constHistogramMap {
		ch <- (*constHistogram).Desc()
	}
}

func (e *CustomHistogram) Collect(ch chan<- prometheus.Metric) {
	e.RLock()
	defer e.RUnlock()
	for _, constHistogram := range e.constHistogramMap {
		ch <- *constHistogram
	}
}

func (e *CustomHistogram) Observe(sum float64, cnt uint64, buckets map[float64]uint64, labels map[string]string, labelKeys []string) {
	e.Lock()
	defer e.Unlock()
	if cnt < 0 {
		log.Errorf("Value for cnt is less than 0 : %v ; Skipping histogram update", cnt)
		return
	}
	labelValues := utils.GetOrderedMapValues(labels, labelKeys)
	histKey := utils.MapToString(labels)
	if e.stateMap[histKey] == nil {
		e.stateMap[histKey] = &CurrentHistogramState{
			Sum:     0,
			Cnt:     0,
			Buckets: make(map[float64]uint64),
		}
	}

	if e.summarize {
		e.stateMap[histKey].Sum += sum
		e.stateMap[histKey].Cnt += cnt
		for key, value := range buckets {
			e.stateMap[histKey].Buckets[key] += value
		}
	} else {
		e.stateMap[histKey].Sum = sum
		e.stateMap[histKey].Cnt = cnt
		for key, value := range buckets {
			e.stateMap[histKey].Buckets[key] = value
		}
	}

	constHistogram := prometheus.MustNewConstHistogram(e.Desc, e.stateMap[histKey].Cnt, e.stateMap[histKey].Sum, e.stateMap[histKey].Buckets, labelValues...)
	e.constHistogramMap[histKey] = &constHistogram
}

func (e *CustomHistogram) DeletePartialMatch(labels prometheus.Labels) {
	e.Lock()
	defer e.Unlock()

	if len(labels) == 0 {
		return
	}

	keysToDelete := []string{}
	for stateKey := range e.stateMap {
		allLabelsPresent := true
		for lKey, lValue := range labels {
			jStr := fmt.Sprintf("%s=\"%s\"", lKey, lValue)
			if !strings.Contains(stateKey, jStr) {
				allLabelsPresent = false
			}
		}

		if allLabelsPresent {
			keysToDelete = append(keysToDelete, stateKey)
		}
	}

	for _, key := range keysToDelete {
		delete(e.stateMap, key)
		delete(e.constHistogramMap, key)
		delete(e.LabelsMap, key)
	}
}
