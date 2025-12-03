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

package tests

import (
	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"

	"github.com/Netcracker/pgskipper-operator/pkg/deployerrors"
	"github.com/Netcracker/pgskipper-operator/pkg/deployment"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/reconciler"
	opUtil "github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
)

var logger = opUtil.GetLogger()

type Creator struct {
	cr      *qubershipv1.PatroniServices
	helper  *helper.Helper
	cluster *patroniv1.PatroniClusterSettings
}

type PatroniCoreCreator struct {
	cr      *patroniv1.PatroniCore
	helper  *helper.PatroniHelper
	cluster *patroniv1.PatroniClusterSettings
}

func NewCreator(cr *qubershipv1.PatroniServices, helper *helper.Helper) *Creator {
	return &Creator{
		cr:     cr,
		helper: helper,
	}
}

func NewCreatorPatroniCore(cr *patroniv1.PatroniCore, helper *helper.PatroniHelper, cluster *patroniv1.PatroniClusterSettings) *PatroniCoreCreator {
	return &PatroniCoreCreator{
		cr:      cr,
		helper:  helper,
		cluster: cluster,
	}
}

func (r *Creator) CreateTestsPods() error {
	cr := r.cr
	if cr.Spec.IntegrationTests != nil {
		integrationTestsPod := deployment.NewIntegrationTestsPod(cr, r.cluster)
		state, err := opUtil.GetPodPhase(integrationTestsPod)
		if err != nil {
			return err
		}
		if state != "Running" {
			if state != "NotFound" {
				if err := r.helper.DeletePodWithWaiting(integrationTestsPod); err != nil {
					logger.Error("Error deleting pod with tests. Let's try to continue.", zap.Error(err))
				}
			}
			if cr.Spec.Policies != nil {
				logger.Info("Policies is not empty, setting them to Test Pod")
				integrationTestsPod.Spec.Tolerations = cr.Spec.Policies.Tolerations
			}
			if err := r.helper.CreatePod(integrationTestsPod); err != nil {
				return err
			}
		}
		state, err = opUtil.WaitForCompletePod(integrationTestsPod)
		if err != nil {
			return &deployerrors.TestsError{Msg: "State of the test pods is unknown."}
		}
		switch state {
		case "Succeeded":
			{
				return nil
			}
		case "Failed":
			{
				return &deployerrors.TestsError{Msg: "Tests pod ended with an error."}
			}
		case "Running":
			{
				return &deployerrors.TestsError{Msg: "Tests pod Phase: Running. Tests take too long to run"}
			}
		case "Pending":
			{
				return &deployerrors.TestsError{Msg: "Tests pod Phase: Pending."}
			}
		default:
			{
				return &deployerrors.TestsError{Msg: "State of the test pods is unknown."}
			}
		}
	}
	return nil
}
