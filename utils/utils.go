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
	"bytes"
	"flag"
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/robfig/cron/v3"
	log "github.com/sirupsen/logrus"
)

var (
	croniterPrecision = flag.String("croniter-precision", "second", "Croniter precision, possible values: second, minute")
	autodiscovery     = flag.String("autodiscovery", GetEnv("AUTODISCOVERY", "false"), "If autodiscovery enabled")
)

func MapToString(m map[string]string) string {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)

	b := new(bytes.Buffer)

	for _, key := range keys {
		fmt.Fprintf(b, "%s=\"%s\",", key, m[key])
	}

	return b.String()
}

func GetKeys(m map[string]string) []string {
	result := make([]string, len(m))
	i := 0
	for key := range m {
		result[i] = key
		i++
	}
	return result
}

func GetOrderedMapValues(m map[string]string, keys []string) []string {
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = m[key]
	}
	return values
}

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func GenerateRandomString(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[rand.Int63()%int64(len(letters))]
	}
	return string(b)
}

func GetCronParser() cron.Parser {
	switch *croniterPrecision {
	case "second":
		log.Info("Parser with second precision created")
		return cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	case "minute":
		log.Info("Parser with minute precision created")
		return cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	default:
		log.Errorf("croniter-precision property has invalid value: %v . Default second precision will be applied for Parser", *croniterPrecision)
		return cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	}
}

func GetCron() *cron.Cron {
	switch *croniterPrecision {
	case "second":
		log.Info("Cron with second precision created")
		return cron.New(cron.WithParser(cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)))
	case "minute":
		log.Info("Cron with minute precision created")
		return cron.New()
	default:
		log.Errorf("croniter-precision property has invalid value: %v . Default second precision will be applied for Cron", *croniterPrecision)
		return cron.New(cron.WithParser(cron.NewParser(cron.SecondOptional | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)))
	}
}

func IsAutodiscoveryEnabled() bool {
	return strings.ToLower(*autodiscovery) == "true"
}

func LabelsCartesian(expectedLabels map[string][]string) []map[string]string {
	sortedLabelNames := getSortedLabelNames(expectedLabels)
	sortedLabelSizes := getSortedLabelSizes(expectedLabels, sortedLabelNames)
	indexes := make([]int64, len(sortedLabelSizes))
	result := make([]map[string]string, 0, multiplyArrayItems(sortedLabelSizes))
	for {
		newLabels := make(map[string]string)

		for i, index := range indexes {
			labelName := sortedLabelNames[i]
			newLabels[labelName] = expectedLabels[labelName][index]
		}
		result = append(result, newLabels)
		if incrementIndexes(indexes, sortedLabelSizes) {
			return result
		}
	}
}

func getSortedLabelNames(expectedLabels map[string][]string) []string {
	labelNames := make([]string, len(expectedLabels))
	i := 0
	for k := range expectedLabels {
		labelNames[i] = k
		i++
	}
	sort.Strings(labelNames)
	return labelNames
}

func getSortedLabelSizes(expectedLabels map[string][]string, sortedLabelNames []string) []int64 {
	labelSizes := make([]int64, len(expectedLabels))
	i := 0
	for _, labelName := range sortedLabelNames {
		labelSizes[i] = int64(len(expectedLabels[labelName]))
		i++
	}
	return labelSizes
}

func multiplyArrayItems(arr []int64) int64 {
	var result int64 = 1
	for _, a := range arr {
		result = result * a
	}
	return result
}

func incrementIndexes(indexes []int64, sortedLabelSizes []int64) bool {
	i := 0
	labelsCount := len(sortedLabelSizes)
	for {
		indexes[i]++
		if indexes[i] < sortedLabelSizes[i] {
			return false
		} else if i == labelsCount-1 {
			return true
		} else {
			indexes[i] = 0
			i++
		}
	}
}
