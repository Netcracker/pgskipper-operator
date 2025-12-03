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

package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Netcracker/pgskipper-backup-daemon/pkg/util"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	serviceName     = "pg-patroni"
	externalCMName  = "postgres-external"
	connectionKey   = "connectionName"
	mirrorSubnetKey = "mirror.subnet"
)

var (
	logger    = util.Logger
	k8sClient crclient.Client
	namespace = ""
)

func init() {
	var err error
	namespace, err = util.GetNamespace()
	if err != nil {
		panic(err)
	}
	k8sClient, err = util.CreateClient()
	if err != nil {
		logger.Error("Can't create k8s client")
		panic(err)
	}
}

func UpdateExternalService(newDBName string) error {
	extService, err := getExternalService()
	if err != nil {
		return err
	}
	extService.Spec.ExternalName = newDBName + ".postgres.database.azure.com"
	if err = k8sClient.Update(context.TODO(), extService); err != nil {
		logger.Error("Can't update external service", zap.Error(err))
		return err
	}
	return nil
}

func UpdateExternalCM(dbName, newDBName string) error {
	extCM, err := GetCM(externalCMName)
	if err != nil {
		return err
	}
	extCM.Data[connectionKey] = strings.ReplaceAll(extCM.Data[connectionKey], dbName, newDBName)
	if err = k8sClient.Update(context.TODO(), extCM); err != nil {
		logger.Error("Can't update external CM", zap.Error(err))
		return err
	}
	return nil
}

func getExternalService() (*corev1.Service, error) {
	extService := &corev1.Service{}
	var k8sErr error
	wait.PollImmediate(3*time.Second, 5*time.Minute, func() (done bool, err error) {
		k8sErr = k8sClient.Get(context.TODO(), types.NamespacedName{
			Name: serviceName, Namespace: namespace,
		}, extService)
		if k8sErr != nil {
			logger.Error("Error during obtaining ext service info, retrying...", zap.Error(err))
			return false, nil
		}
		return true, nil
	})
	if k8sErr != nil {
		logger.Error("Timeout exceeded", zap.Error(k8sErr))
	}
	return extService, k8sErr
}

func GetServerName() (string, error) {
	cm, err := GetCM(externalCMName)
	if err != nil {
		return "", err
	}
	fullName := cm.Data[connectionKey]
	splitStr := strings.Split(fullName, ".")
	return splitStr[0], nil
}

func GetCM(cmName string) (*corev1.ConfigMap, error) {
	cm := &corev1.ConfigMap{}
	var k8sErr error
	wait.PollImmediate(3*time.Second, 5*time.Minute, func() (done bool, err error) {
		k8sErr = k8sClient.Get(context.TODO(), types.NamespacedName{
			Name: cmName, Namespace: namespace,
		}, cm)
		if k8sErr != nil {
			logger.Error(fmt.Sprintf("Error during obtaining %s ConfigMap, retrying...", cmName), zap.Error(err))
			return false, nil
		}
		return true, nil
	})
	if k8sErr != nil {
		logger.Error("Timeout exceeded", zap.Error(k8sErr))
	}
	return cm, k8sErr
}

// This CM must be created by Velero in case environment has specific type
func IsEnvTypeCmExist(cmName string) (bool, error) {
	err := wait.PollImmediate(3*time.Second, 5*time.Minute, func() (done bool, err error) {
		cm := &corev1.ConfigMap{}
		k8sErr := k8sClient.Get(context.TODO(), types.NamespacedName{
			Name: cmName, Namespace: namespace,
		}, cm)
		if k8sErr != nil {
			if errors.IsNotFound(k8sErr) {
				logger.Error(fmt.Sprintf("Cm %s is not found", cmName))
				return false, k8sErr
			}
			logger.Error(fmt.Sprintf("Error during obtaining %s ConfigMap, retrying...", cmName), zap.Error(err))
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func UpdateCMData(cmName string, dataForUpdate map[string]string) error {
	cm, err := GetCM(cmName)
	if err != nil {
		return err
	}
	for key, value := range dataForUpdate {
		cm.Data[key] = value
	}

	err = k8sClient.Update(context.TODO(), cm)
	if err != nil {
		logger.Error("Error during updating ext CM", zap.Error(err))
		return err
	}
	return nil
}
