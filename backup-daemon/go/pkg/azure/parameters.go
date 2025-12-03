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

package azure

import (
	"context"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	server "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/postgresql/armpostgresqlflexibleservers/v2"
	"go.uber.org/zap"
)

func (azCl *Client) updateNewServerParameters(ctx context.Context, client *server.ServersClient, oldServer server.ServersClientGetResponse) error {
	HAMode := oldServer.Properties.HighAvailability.Mode
	tags := oldServer.Tags
	resourceGroup := azCl.ResourceGroup()
	if azCl.IsMirrorRestore() {
		resourceGroup = azCl.MirrorResourceGroup()
		logger.Info("turning off HA, because we do override of subnet")
		HAMode = to.Ptr(server.HighAvailabilityModeDisabled)
		tags[TypeTagKey] = to.Ptr(MirrorMode)
	} else if azCl.IsGeoRestore() {
		resourceGroup = azCl.GeoResourceGroup()
		geoHaModeStr := azCl.GeoHA()
		if geoHaModeStr != "" {
			logger.Info("changing HA mode, due to configuration parameters")
			geoHaMode := server.HighAvailabilityMode(geoHaModeStr)
			HAMode = &geoHaMode
		}
		tags[TypeTagKey] = to.Ptr(DrMode)
	}
	retentionDays := oldServer.Properties.Backup.BackupRetentionDays
	logger.Info(fmt.Sprintf("Setting new server HA parameters: mode=%s, backup retension days=%d", *HAMode, *retentionDays))
	poller, err := client.BeginUpdate(ctx,
		resourceGroup,
		azCl.NewServerName(),
		server.ServerForUpdate{
			Properties: &server.ServerPropertiesForUpdate{
				HighAvailability: &server.HighAvailability{
					Mode: HAMode,
				},
				Backup: &server.Backup{BackupRetentionDays: retentionDays},
			},
			Tags: tags,
		},
		nil)

	if err != nil {
		logger.Error("cannot update new Flexible Postgres database", zap.Error(err))
		panic(err)
	}

	resUpdate, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		logger.Error("error during polling HA update", zap.Error(err))
		panic(err)
	}

	_ = resUpdate

	return nil
}
