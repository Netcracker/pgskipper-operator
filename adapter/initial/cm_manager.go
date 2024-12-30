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

package initial

import (
	"context"
	"fmt"
	"os"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v12 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var log = util.GetLogger()

func getCM(cmName string) *v1.ConfigMap {
	CMs := getCMListForNamespace()
	cm, err := CMs.Get(context.Background(), cmName, metav1.GetOptions{})
	if err != nil {
		log.Error(fmt.Sprintf("Couldn't get %s config map", cmName), zap.Error(err))
	}
	return cm
}

func updateCM(cm *v1.ConfigMap) {
	CMs := getCMListForNamespace()
	_, err := CMs.Update(context.Background(), cm, metav1.UpdateOptions{})
	if err != nil {
		log.Error("Couldn't update config map", zap.Error(err))
	}
}

func getCMListForNamespace() v12.ConfigMapInterface {
	client, err := util.GetK8sClient()
	if err != nil {
		return nil
	}
	CMs := client.CoreV1().ConfigMaps(os.Getenv("CLOUD_NAMESPACE"))
	return CMs
}
