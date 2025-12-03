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
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	azlog "github.com/Azure/azure-sdk-for-go/sdk/azcore/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const nsPath = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

var (
	Logger         = getLogger()
	AzureLogEvents = []azlog.Event{azlog.EventRequest, azlog.EventResponse, azlog.EventLRO, azlog.EventRetryPolicy}
)

func getLogger() *zap.Logger {
	atom := zap.NewAtomicLevel()
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	))
	defer logger.Sync()
	return logger
}

func CreateClient() (crclient.Client, error) {
	clientConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	UpdateTransport(clientConfig)
	client, err := crclient.New(clientConfig, crclient.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func ReadConfigFile(filePath string) (map[string]string, error) {
	file, err := os.ReadFile(filePath)
	if err != nil {
		Logger.Error(fmt.Sprintf("cannot read config file: %s", filePath))
		return nil, err
	}
	var externalConfig map[string]string
	err = json.Unmarshal(file, &externalConfig)
	if err != nil {
		Logger.Error(fmt.Sprintf("Failed to parse config file %s", filePath), zap.Error(err))
		return nil, err
	}
	return externalConfig, nil
}

func WriteFile(filePath string, data interface{}) error {
	file, err := os.Create(filePath)
	if err != nil {
		Logger.Error(fmt.Sprintf("cannot create %s file", filePath))
		return err
	}
	defer file.Close()

	dataStr, err := json.Marshal(data)
	if err != nil {
		Logger.Error("Cannot convert data to string")
		return err
	}
	_, err = file.Write(dataStr)
	if err != nil {
		Logger.Error(fmt.Sprintf("cannot write into %s file", filePath))
		return err
	}
	return nil
}

func DeleteFile(filepath string) {
	err := os.Remove(filepath)
	if err != nil {
		Logger.Error("cannot remove current task file")
	}
}

func ConfigureAzLogging() {
	azlog.SetListener(func(cls azlog.Event, msg string) {
		prefixLog := "received event: "
		switch cls {
		case azlog.EventLRO:
			prefixLog = "long running event: "
		case azlog.EventRetryPolicy:
			prefixLog = "retry event: "
		}
		Logger.Info("[azlog]" + prefixLog + msg)
	})
	azlog.SetEvents(AzureLogEvents...)
}

func getEnvInt(name string, defValue int) int {
	val := os.Getenv(name)
	if val == "" {
		return defValue
	}
	intVal, err := strconv.ParseInt(val, 10, 32)
	if err != nil {
		Logger.Warn(fmt.Sprintf("cannot parse %s env variable, value %d will be used", name, defValue))
		return defValue
	}
	return int(intVal)
}

func GetNamespace() (string, error) {
	return ReadFromFile(nsPath)
}

func ReadFromFile(filePath string) (string, error) {
	dat, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}
	return string(dat), nil
}
