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
	log "github.com/sirupsen/logrus"
	"os"
	"strconv"
)

func GetOctalUintEnvironmentVariable(name string, defaultValue uint32) uint32 {
	valueStr := os.Getenv(name)
	if valueStr == "" {
		log.Infof("Environment variable %v is empty. Default value %o is used", name, defaultValue)
		return defaultValue
	}
	result, err := strconv.ParseUint(valueStr, 8, 32)

	if err != nil {
		log.Errorf("Error trying to parse uint octal environment variable %v with value %v, default value %o is used instead. Error : %+v", name, valueStr, defaultValue, err)
		return defaultValue
	}

	log.Infof("Environment variable %v with value %v parsed successfully as octal uint %o", name, valueStr, result)

	return uint32(result)

}

func GetEnv(key string, defaultVal string) string {
	if value, exists := os.LookupEnv(key); exists {
		log.Infof("Values for %s is %s", key, value)
		return value
	}

	return defaultVal
}

func GetEnvBool(key string, defaultVal bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		result, err := strconv.ParseBool(value)
		if err != nil {
			log.Errorf("Cannot parse %s with value %s to bool, set %t ; err : %+v", key, value, defaultVal, err)
			return defaultVal
		}
		log.Infof("Env variable %s has value %t", key, result)
		return result
	}

	return defaultVal
}

func GetEnvInt(key string, defaultVal int) int {
	if value, exists := os.LookupEnv(key); exists {
		result, err := strconv.ParseInt(value, 10, 32)
		if err != nil {
			log.Errorf("Cannot parse %s with value %s to int, set %d ; err : %+v", key, value, defaultVal, err)
			return defaultVal
		}
		log.Infof("Env variable %s has value %d", key, result)
		return int(result)
	}

	return defaultVal
}
