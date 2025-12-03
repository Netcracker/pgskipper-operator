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

package storage

import (
	"fmt"
	commonv1 "github.com/Netcracker/pgskipper-operator/api/common/v1"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"strings"
)

var (
	logger = util.GetLogger()
)

func NewPvc(pvcName string, storageEntity *commonv1.Storage, idx int) *corev1.PersistentVolumeClaim {
	var pvcSpec corev1.PersistentVolumeClaimSpec
	switch storageEntity.Type {
	case "provisioned":
		//logger.Info(fmt.Sprintf("Storage type is set to provisioned, will use %s as a storageEntity class", storageEntity.StorageClass))
		pvcSpec = corev1.PersistentVolumeClaimSpec{
			Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceName(corev1.ResourceStorage): resource.MustParse(storageEntity.Size)}},
			AccessModes:      getAccessModes(storageEntity.AccessModes),
			StorageClassName: &storageEntity.StorageClass,
		}
	case "pv":
		//logger.Info("Storage type is set to pv")
		pvcSpec = corev1.PersistentVolumeClaimSpec{
			Resources:        corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceName(corev1.ResourceStorage): resource.MustParse(storageEntity.Size)}},
			AccessModes:      getAccessModes(storageEntity.AccessModes),
			StorageClassName: new(string),
		}
		if storageEntity.StorageClass != "" {
			//logger.Info("StorageClass is not empty and type is set to PV, will set storageClass to PVC")
			pvcSpec.StorageClassName = &storageEntity.StorageClass
		}

		if len(storageEntity.Selectors) > 0 {
			keyValue := strings.Split(storageEntity.Selectors[idx-1], "=")
			pvcSpec.Selector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					keyValue[0]: keyValue[1],
				},
			}
		} else if len(storageEntity.Volumes) >= idx {
			pvcSpec.VolumeName = storageEntity.Volumes[idx-1]
		} else {
			logger.Info(fmt.Sprintf("The volume for PVC %s is not specified!!!", pvcName))
		}

	}

	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: util.GetNameSpace(),
		},
		Spec: pvcSpec,
	}
	return pvc
}

func GetConfigMapByName(configMapName string, configMapKey string) *corev1.ConfigMap {
	namespace := util.GetNameSpace()
	filePath := fmt.Sprintf("/opt/operator/%s", configMapName)
	bytes, e := os.ReadFile(filePath)
	if e != nil {
		logger.Error("Failed to read from file", zap.Error(e))
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			configMapKey: string(bytes),
		},
	}
}

func getAccessModes(accessModes []string) []corev1.PersistentVolumeAccessMode {
	if len(accessModes) == 0 {
		return []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
	}

	result := make([]corev1.PersistentVolumeAccessMode, 0, 1)
	for _, mode := range accessModes {
		switch mode {
		case string(corev1.ReadWriteOnce):
			result = append(result, corev1.ReadWriteOnce)
		case string(corev1.ReadWriteMany):
			result = append(result, corev1.ReadWriteMany)
		case string(corev1.ReadOnlyMany):
			result = append(result, corev1.ReadOnlyMany)
		default:
			logger.Info(fmt.Sprintf("Skipping unknown AccessMode: %s", mode))
		}
	}
	return result
}
