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

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/gauges"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/k8s"
	"github.com/Netcracker/pgskipper-monitoring-agent/collector/pkg/util"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"go.uber.org/zap"
)

var (
	mapping = map[string]int{
		"up":         0,
		"successful": 0,
		"ok":         0,
		"warning":    3,
		"planned":    4,
		"inprogress": 5,
		"problem":    6,
		"off":        0,
		"on":         1,
		"crit":       10,
		"fatal":      10,
		"failed":     10,
		"down":       14,
		"unknown":    -1,
	}
)

func (s *Scraper) CollectMetricsFromCM() {
	logger.Debug("Start Collect Services metrics from CMs")
	defer s.HandleMetricCollectorStatus()

	cms := k8s.GetConfigMaps()
	for _, configMap := range cms.Items {

		if len(configMap.Name) > 16 {
			andsWith := configMap.Name[len(configMap.Name)-16:]
			if andsWith == "collector-config" {
				logger.Info(fmt.Sprintf("start to process cm: %s", configMap.Name))
				s.processConfigMap(configMap.Data)
			}
		}
	}
}

func (s *Scraper) processUrlCollect(url string) []byte {

	logger.Debug(fmt.Sprintf("Process url: %s to fetch metrics", url))

	status, body, err := util.ProcessHttpRequest(s.httpClient, url, s.token)
	if err != nil {
		logger.Error(fmt.Sprintf("Cannot collect service status metric. url %v", url), zap.Error(err))
		return []byte("")
	}
	statusCode, _ := strconv.Atoi(status)
	if statusCode >= 400 {
		logger.Warn(fmt.Sprintf("Cannot collect service status metric. Error code %v", statusCode))
	}
	return body

}

func parseMF(data string) (map[string]*dto.MetricFamily, error) {

	var parser expfmt.TextParser
	mf, err := parser.TextToMetricFamilies(strings.NewReader(data))
	if err != nil {
		return nil, err
	}
	return mf, nil
}

func (s *Scraper) getPrometheusMetrics(url string, module map[string]interface{}) {
	logger.Debug(fmt.Sprintf("Process prometheus metrics with url: %s", url))
	data := s.processUrlCollect(url)
	if len(data) == 0 {
		Log.Warn("Returned Empty Data, skipping.")
		return
	}
	families, err := parseMF(string(data))
	if err != nil {
		Log.Error(fmt.Sprintf("Parce prometheus metrics error: %s", err))
		return
	} else {
		labels := gauges.DefaultLabels()
		labels["pod_name"] = s.getPodName(module)
		labels["service_name"] = module["parameters"].(map[string]interface{})["service_name"].(string)
		for _, family := range families {
			for _, metric := range family.Metric {
				if metric.Gauge == nil {
					continue
				}
				m := NewMetric(fmt.Sprintf("ma_%s", *family.Name)).withLabels(labels).setValue(*metric.Gauge.Value)

				s.metrics = append(s.metrics, m)
			}
		}
	}
}

func (s *Scraper) processConfigMap(data map[string]string) {

	for _, v := range data {
		var res []map[string]interface{}
		err := json.Unmarshal([]byte(v), &res)
		if err != nil {
			Log.Error(fmt.Sprintf("Process Config Map Unmarshal Error: %s", err))
			continue
		}
		for _, module := range res {
			if parameters, ok := module["parameters"].(map[string]interface{}); ok {
				paramType, ok := parameters["type"]
				if !ok {
					paramType = "url"
				}
				metricType, ok := parameters["metrics_type"]
				if !ok {
					metricType = "json"
				}
				err = handleSourceOfMetric(s, parameters, paramType, metricType, module)
				if err != nil {
					continue
				}
			}

		}
	}
}

func handleSourceOfMetric(s *Scraper, parameters map[string]interface{}, paramType interface{}, metricType interface{}, module map[string]interface{}) error {
	var jsonData map[string]interface{}
	if paramType == "url" {
		url := parseSSLMode(parameters["url"].(string))
		if metricType == "json" {
			byteData := s.processUrlCollect(url)
			if len(byteData) != 0 {
				err := json.Unmarshal(byteData, &jsonData)
				if err != nil {
					Log.Error(fmt.Sprintf("Process Config Map Unmarshal Error: %s", err))
					return err
				}
				s.handleJsonData(jsonData, module)
				return nil
			}
		} else {
			s.getPrometheusMetrics(url, module)
			return nil
		}
	}
	return nil
}

func parseSSLMode(url string) string {
	if util.GetEnv("PGSSLMODE", "disable") == "require" {
		return strings.Replace(url, "http", "https", -1)
	}
	return url
}

func linearizeJson(obj interface{}) map[string]interface{} {

	res := make(map[string]interface{})
	walk(obj, "", res)
	return res
}

func walk(obj interface{}, prefix string, res map[string]interface{}) {
	propKey := ""
	switch o := obj.(type) {
	default:
		res[prefix] = obj
	case map[string]interface{}:
		for k, v := range o {

			if prefix == "" {
				propKey = k
			} else {
				propKey = fmt.Sprintf("%s_%s", prefix, k)
			}
			walk(v, propKey, res)
		}
	case []interface{}:
		for i, item := range o {
			if prefix == "" {
				propKey = strconv.Itoa(i)
			} else {
				propKey = fmt.Sprintf("%s_%v", prefix, i)
			}
			walk(item, propKey, res)
		}
	}
}

func mapStatuses(lines map[string]interface{}) {

	for k, v := range lines {
		if k == "status" || (len(k) > 7 && k[len(k)-7:] == "_status") {
			for status, value := range mapping {
				if strings.ToLower(v.(string)) == status {
					lines[k] = value
					break
				} else {
					lines[k] = mapping["unknown"]
				}
			}
		}
	}

}

func (s *Scraper) handleJsonData(json, module map[string]interface{}) {

	podName := s.getPodName(module)
	labels := gauges.DefaultLabels()
	labels["selector"] = module["parameters"].(map[string]interface{})["output_selector"].(string)
	labels["service_name"] = module["parameters"].(map[string]interface{})["service_name"].(string)
	labels["pod_name"] = podName

	points := linearizeJson(json)
	mapStatuses(points)
	for metricName, value := range points {

		mName := fmt.Sprintf("ma_%s", strings.Replace(metricName, ".", "_", -1))
		metric := NewMetric(mName).withLabels(labels).setValue(value)
		s.metrics = append(s.metrics, metric)
	}
}

func (s *Scraper) getPodName(module map[string]interface{}) string {

	lableKey := module["parameters"].(map[string]interface{})["selector"].(map[string]interface{})["key"].(string)
	lableValue := module["parameters"].(map[string]interface{})["selector"].(map[string]interface{})["value"].(string)
	commonLabels := map[string]string{lableKey: lableValue}

	podList, _ := util.GetPodList(s.client, namespace, commonLabels)
	return podList.Items[0].Name
}
