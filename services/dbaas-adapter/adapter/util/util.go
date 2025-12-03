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
	"context"
	"flag"
	"fmt"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"os"
	"strconv"
)

const (
	MaxPgDBLength          = 63
	MaxAllowedSuffixLength = 12

	rolesVersion = 1.0
)

var (
	log                    *zap.Logger
	isDebugEnabled         = *flag.Bool("log_debug", GetEnvBool("LOG_DEBUG", false), "If debug logs is enabled, env: LOG_DEBUG")
	ExternalPostgreSQLType = *flag.String("cloud_type", GetEnv("EXTERNAL_POSTGRESQL", ""), "A type of cloud where postgres is deployed, env: EXTERNAL_POSTGRESQL")
)

func ContextLogger(ctx context.Context) *zap.Logger {
	//loggerCtx := ctx.Value("logger")
	//if loggerCtx != nil {
	//	return loggerCtx.(zap.Logger)
	//}
	logger := GetLogger()
	return logger.With(zap.ByteString("request_id", []byte(fmt.Sprintf("%s", ctx.Value("request_id")))))
}

func GetLogger() *zap.Logger {
	//if log != nil {
	//	return log
	//}

	atom := zap.NewAtomicLevel()
	if isDebugEnabled {
		atom = zap.NewAtomicLevelAt(zap.DebugLevel)
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	))
	defer func() {
		_ = logger.Sync()
	}()
	log = logger
	return logger
}

func Contains(array []string, str string) bool {
	for _, a := range array {
		if a == str {
			return true
		}
	}
	return false
}

func GetStringArrayFromInterface(inter interface{}) []string {
	if inter == nil {
		return make([]string, 0)
	}
	interfaceArray := inter.([]interface{})
	resultArray := make([]string, len(interfaceArray))
	for i := range interfaceArray {
		resultArray[i] = interfaceArray[i].(string)
	}
	return resultArray
}

func GetMapStringFromMapInterface(inter interface{}) map[string]string {
	if inter == nil {
		return make(map[string]string)
	}
	rawMap := inter.(map[string]interface{})
	resultMap := make(map[string]string)
	for i, v := range rawMap {
		resultMap[i] = v.(string)
	}
	return resultMap
}

func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func GetEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if ivalue, err := strconv.Atoi(value); err == nil {
			return ivalue
		}
	}
	return fallback
}

func GetEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		bvalue, err := strconv.ParseBool(value)
		if err != nil {
			log.Error(fmt.Sprintf("Can't parse %s boolean variable", key), zap.Error(err))
			panic(err)
		}
		return bvalue
	}
	return fallback
}

func GetK8sClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Error("Couldn't get k8s config", zap.Error(err))
		return nil, err
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Error("Couldn't get k8s client", zap.Error(err))
		return nil, err
	}

	return client, nil
}

func GetPgDBLength() int {
	return MaxPgDBLength
}

func IsExternalPostgreSQl() bool {
	return ExternalPostgreSQLType != ""
}

func GetRolesVersion() float64 {
	return rolesVersion
}
