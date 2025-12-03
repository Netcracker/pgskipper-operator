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
	"math"
	"strconv"

	"github.com/Netcracker/qubership-query-exporter/config"
	log "github.com/sirupsen/logrus"
)

type MetricDefaultValues struct {
	DefaultValue                 float64
	DefaultValueQueryFailed      float64
	DefaultValueQueryReturnedNaN float64
}

func CreateMetricDefaultValues(metricName string, defaultValue string, defaultValueQueryFailed string, defaultValueQueryReturnedNaN string) *MetricDefaultValues {
	mdv := MetricDefaultValues{}

	val, err := strconv.ParseFloat(defaultValue, 64)
	if err != nil {
		if defaultValue == "" {
			log.Debugf("Empty defaultValue received for metric %v; defaultValue is set to NaN", metricName)
		} else {
			log.Errorf("Error parsing defaultValue %v for metric %v : %+v; defaultValue is set to NaN", defaultValue, metricName, err)
		}
		mdv.DefaultValue = math.NaN()
	} else {
		mdv.DefaultValue = val
	}

	val, err = strconv.ParseFloat(defaultValueQueryFailed, 64)
	if err != nil {
		if defaultValueQueryFailed == "" {
			log.Debugf("Empty defaultValueQueryFailed received for metric %v; defaultValueQueryFailed is set to NaN", metricName)
		} else {
			log.Errorf("Error parsing defaultValueQueryFailed %v for metric %v : %+v; defaultValueQueryFailed is set to NaN", defaultValueQueryFailed, metricName, err)
		}
		mdv.DefaultValueQueryFailed = math.NaN()
	} else {
		mdv.DefaultValueQueryFailed = val
	}

	val, err = strconv.ParseFloat(defaultValueQueryReturnedNaN, 64)
	if err != nil {
		if defaultValueQueryReturnedNaN == "" {
			log.Debugf("Empty defaultValueQueryReturnedNaN received for metric %v; defaultValueQueryReturnedNaN is set to NaN", metricName)
		} else {
			log.Errorf("Error parsing defaultValueQueryReturnedNaN %v for metric %v : %+v; defaultValueQueryReturnedNaN is set to NaN", defaultValueQueryReturnedNaN, metricName, err)
		}
		mdv.DefaultValueQueryReturnedNaN = math.NaN()
	} else {
		mdv.DefaultValueQueryReturnedNaN = val
	}

	return &mdv
}

type MetricDefaultValuesRepository struct {
	m map[string]*MetricDefaultValues
}

func CreateMetricDefaultValuesRepository(metricsMap map[string]*config.MetricsConfig) *MetricDefaultValuesRepository {
	repo := MetricDefaultValuesRepository{}
	repo.m = make(map[string]*MetricDefaultValues)

	for metricName, metricCfg := range metricsMap {
		if len(metricCfg.Parameters) == 0 {
			log.Debugf("DefaultValues : No parameters found for metric %v", metricName)
			continue
		}
		defaultValue := metricCfg.Parameters["default-value"]
		defaultValueQueryFailed := metricCfg.Parameters["default-value-query-failed"]
		defaultValueQueryReturnedNaN := metricCfg.Parameters["default-value-query-returned-nan"]
		if defaultValue == "" && defaultValueQueryFailed == "" && defaultValueQueryReturnedNaN == "" {
			log.Debugf("DefaultValues : No default values found for metric %v", metricName)
			continue
		}
		mdv := CreateMetricDefaultValues(metricName, defaultValue, defaultValueQueryFailed, defaultValueQueryReturnedNaN)
		repo.m[metricName] = mdv
		log.Infof("Default values successfully created for metric %v : %+v", metricName, mdv)
	}

	return &repo
}

/*
func (repo *MetricDefaultValuesRepository) GetMetricDefaultValues(metric string) {
    return repo.m[metric]
} */

func (repo *MetricDefaultValuesRepository) GetMetricDefaultValue(metric string) float64 {
	if repo.m[metric] == nil {
		log.Tracef("For metric %v DefaultValue is NaN", metric)
		return math.NaN()
	}

	return repo.m[metric].DefaultValue
}

func (repo *MetricDefaultValuesRepository) GetMetricDefaultValueQueryFailed(metric string) float64 {
	if repo.m[metric] == nil {
		log.Tracef("For metric %v DefaultValueQueryFailed is NaN", metric)
		return math.NaN()
	}

	return repo.m[metric].DefaultValueQueryFailed
}

func (repo *MetricDefaultValuesRepository) GetMetricDefaultValueQueryReturnedNaN(metric string) float64 {
	if repo.m[metric] == nil {
		log.Tracef("For metric %v DefaultValueQueryReturnedNaN is NaN", metric)
		return math.NaN()
	}

	return repo.m[metric].DefaultValueQueryReturnedNaN
}
