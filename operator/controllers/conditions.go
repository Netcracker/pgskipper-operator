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

package controllers

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	InProgress string = "In progress"
	Successful string = "Successful"
	Failed     string = "Failed"
)

func (r *PostgresServiceReconciler) forceUpdateStatus(cr *v1.PatroniServices, statusType string, reason string, message string) bool {
	transitionTime := metav1.Now()
	newCondition := v1.PatroniServicesStatusCondition{
		Type:               statusType,
		Status:             true,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: transitionTime.String(),
	}
	if len(cr.Status.Conditions) == 0 {
		cr.Status.Conditions = append(cr.Status.Conditions, newCondition)
	} else {
		cr.Status.Conditions[0] = newCondition
	}
	return true
}

func (r *PostgresServiceReconciler) updateStatus(statusType string, reason string, message string) error {
	newCr, err := r.helper.GetPostgresServiceCR()
	if r.forceUpdateStatus(newCr, statusType, reason, message) {
		// Update status if not equal to the last one
		r.logger.Info(fmt.Sprintf("Update operator status. statusType: %s, reason: %s, message: %s", statusType, reason, message))
		err := wait.PollUntilContextTimeout(context.Background(), time.Second, 1*time.Minute, true, func(ctx context.Context) (done bool, err error) {
			if e := r.Client.Status().Update(context.TODO(), newCr); e != nil {
				r.logger.Error(fmt.Sprintf("Can't Update operator status. Error: %s, Retrying.", err))
				return false, err
			} else {
				return true, nil
			}
		})
		return err
	}
	return err
}

func (pr *PatroniCoreReconciler) updateStatus(statusType string, reason string, message string) error {
	newCr, err := pr.helper.GetPatroniCoreCR()
	if pr.forceUpdateStatus(newCr, statusType, reason, message) {
		// Update status if not equal to the last one
		pr.logger.Info(fmt.Sprintf("Update operator status. statusType: %s, reason: %s, message: %s", statusType, reason, message))
		err := wait.PollUntilContextTimeout(context.Background(), time.Second, 1*time.Minute, true, func(ctx context.Context) (done bool, err error) {
			if e := pr.Client.Status().Update(context.TODO(), newCr); e != nil {
				pr.logger.Error(fmt.Sprintf("Can't Update operator status. Error: %s, Retrying.", err))
				return false, err
			} else {
				return true, nil
			}
		})
		return err
	}
	return err
}

func (p *PatroniCoreReconciler) forceUpdateStatus(cr *patroniv1.PatroniCore, statusType string, reason string, message string) bool {
	transitionTime := metav1.Now()
	newCondition := patroniv1.PatroniCoreStatusCondition{
		Type:               statusType,
		Status:             true,
		Reason:             reason,
		Message:            message,
		LastTransitionTime: transitionTime.String(),
	}
	if len(cr.Status.Conditions) == 0 {
		cr.Status.Conditions = append(cr.Status.Conditions, newCondition)
	} else {
		cr.Status.Conditions[0] = newCondition
	}
	return true
}
