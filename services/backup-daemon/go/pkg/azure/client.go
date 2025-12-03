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
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	server "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/postgresql/armpostgresqlflexibleservers/v2"
	"github.com/Netcracker/pgskipper-backup-daemon/pkg/config"
	"github.com/Netcracker/pgskipper-backup-daemon/pkg/k8s"
	"github.com/Netcracker/pgskipper-backup-daemon/pkg/util"
	"go.uber.org/zap"
)

const (
	configFile        = "/app/config/config.json"
	currentStatusFile = "status"
	statusSuccessful  = "Successful"
	statusInProgress  = "In Progress"
	statusFailed      = "Failed"

	resourceGroup  = "resourceGroup"
	subscriptionId = "subscriptionId"
	tenantId       = "tenantId"
	clientSecret   = "clientSecret"
	clientId       = "clientId"

	restoreConfigCM = "external-restore-config"
	mirrorCM        = "mirror-config"
	DRCM            = "dr-config"
)

var (
	logger = util.Logger
)

type Client struct {
	clientConfig
	*config.RestoreConfig
	creds *azidentity.ClientSecretCredential
}

const (
	TypeTagKey = "type"

	DrMode     = "dr"
	MirrorMode = "mirror"
)

type Status struct {
	ServerName    string `json:"serverName"`
	NewServerName string `json:"newServerName"`
	Status        string `json:"status"`
	RestoreId     string `json:"restoreId"`
	StatusFolder  string `json:"-"`
}

type clientConfig map[string]string

func (c clientConfig) SubscribtionId() string {
	return c[subscriptionId]
}

func NewRestoreClientWithRestoreConfig(restoreCfg *config.RestoreConfig) *Client {
	config, err := util.ReadConfigFile(configFile)
	if err != nil {
		panic(err)
	}

	return &Client{
		clientConfig:  config,
		RestoreConfig: restoreCfg,
	}
}

func (azCl *Client) clientInfo(name string) string {
	return azCl.clientConfig[name]
}

func (azCl *Client) getStatus(status string) Status {
	return Status{
		ServerName:    azCl.ServerName(),
		NewServerName: azCl.NewServerName(),
		Status:        status,
		RestoreId:     azCl.RestoreID(),
		StatusFolder:  azCl.StatusFolder(),
	}
}

func (azCl *Client) RestoreDatabase() {
	status := azCl.getStatus(statusInProgress)
	defer checkErrorStatus(status)

	ctx := context.Background()
	client := azCl.getAzureClient()
	oldServer, err := client.Get(ctx, azCl.ResourceGroup(), azCl.ServerName(), nil)
	if err != nil {
		logger.Error("cannot connect to Azure server", zap.Error(err))
		panic(err)
	}
	setStatus(status)

	azCl.checkRestoreValidity(oldServer)

	azCl.createNewServerFromRP(ctx, client, oldServer)
	err = azCl.updateNewServerParameters(ctx, client, oldServer)
	if err != nil {
		panic(err)
	}

	if azCl.StopSource() {
		azCl.stopServer(ctx, client)
	}

	if azCl.IsGeoRestore() {
		dataToUpdate := make(map[string]string)
		dataToUpdate[config.GeoHaKey] = string(*oldServer.Properties.HighAvailability.Mode)
		dataToUpdate[config.GeoSubnetKey] = *oldServer.Properties.Network.DelegatedSubnetResourceID
		dataToUpdate[config.GeoLocationKey] = azCl.Location()
		dataToUpdate[config.GeoRGKey] = azCl.ResourceGroup()
		dataToUpdate[config.GeoPrivateDNSZone] = *oldServer.Properties.Network.PrivateDNSZoneArmResourceID
		dataToUpdate[config.MainLocationKey] = azCl.GeoLocation()
		k8s.UpdateCMData(restoreConfigCM, dataToUpdate)
	}

	status.Status = statusSuccessful
	setStatus(status)
}

func (azCl *Client) checkRestoreValidity(oldServer server.ServersClientGetResponse) {
	isMirror, isDr := azCl.getSiteModes()
	// check mirror restrictions
	if isMirror {
		logger.Info("Mirror config is found, only mirror restore of source server and restore for mirror server are allowed")
		tagActualVal, tagIsPresent := oldServer.Tags[TypeTagKey]
		isMirrorServer := tagIsPresent && *tagActualVal == MirrorMode
		if isMirrorServer {
			logger.Info("Pg server for restore is a mirror")
		} else {
			logger.Info("Pg server for restore is not a mirror")
		}
		if !isMirrorServer && !azCl.IsMirrorRestore() {
			errorMsg := fmt.Sprintf("Cannot perform only mirror restore on %s from mirror side", *oldServer.Name)
			logger.Error(errorMsg)
			panic(errorMsg)
		}
	}
	// check dr restrictions
	if isDr {
		logger.Info("Dr config is found, only dr restore of source server and restore for dr server are allowed")
		tagActualVal, tagIsPresent := oldServer.Tags[TypeTagKey]
		isDrServer := tagIsPresent && *tagActualVal == DrMode
		if isDrServer {
			logger.Info("Pg server for restore is a dr")
		} else {
			logger.Info("Pg server for restore is not a dr")
		}
		if !isDrServer && !azCl.IsGeoRestore() {
			errorMsg := fmt.Sprintf("Cannot perform only dr restore on %s from dr side", *oldServer.Name)
			logger.Error(errorMsg)
			panic(errorMsg)
		}
	}
}

func (azCl *Client) getSiteModes() (isMirror bool, isDR bool) {
	isMirror, err := k8s.IsEnvTypeCmExist(mirrorCM)
	if err != nil {
		panic(err)
	}

	isDR, err = k8s.IsEnvTypeCmExist(DRCM)
	if err != nil {
		panic(err)
	}

	return
}

func (azCl *Client) getAzureClient() *server.ServersClient {
	client, err := server.NewServersClient(azCl.clientInfo(subscriptionId), azCl.getAzureCreds(), nil)
	if err != nil {
		logger.Error(fmt.Sprintf("failed to get server client: %v", err))
		panic(err)
	}
	return client
}

func (azCl *Client) getAzureCreds() *azidentity.ClientSecretCredential {
	if azCl.creds == nil {
		creds, err := azidentity.NewClientSecretCredential(azCl.clientInfo(tenantId), azCl.clientInfo(clientId), azCl.clientInfo(clientSecret), nil)
		if err != nil {
			logger.Error(fmt.Sprintf("failed to obtain a credential: %v", err))
			panic(err)
		}
		azCl.creds = creds
	}
	return azCl.creds
}

func (azCl *Client) createNewServerFromRP(ctx context.Context, client *server.ServersClient, oldServer server.ServersClientGetResponse) {
	logger.Info(fmt.Sprintf("Starting restore of database %s with new name %s by point in time %s", *oldServer.Name, azCl.NewServerName(), azCl.RestoreTime()))
	locationName := *oldServer.Location
	avZone := oldServer.Properties.AvailabilityZone
	restoreType := server.CreateModePointInTimeRestore
	subnet := oldServer.Properties.Network.DelegatedSubnetResourceID
	backup := oldServer.Properties.Backup
	resourceGroup := azCl.ResourceGroup()
	privateDnsZone := oldServer.Properties.Network.PrivateDNSZoneArmResourceID
	// Select restore mode
	if azCl.IsGeoRestore() {
		locationName = azCl.GeoLocation()
		resourceGroup = azCl.GeoResourceGroup()
		restoreType = server.CreateModeGeoRestore
		subnet = to.Ptr(azCl.GeoSubnet())
		avZone = nil
		if len(azCl.GeoPrivateDNSZone()) != 0 {
			logger.Info(fmt.Sprintf("geo restore in progress and dnsZone is specified, "+
				"overriding it to: %s", azCl.GeoPrivateDNSZone()))
			privateDnsZone = to.Ptr(azCl.GeoPrivateDNSZone())
		}
		// Disable Geo-Redundant backup on restored server due to Azure restrictions
		disabledEnum := server.GeoRedundantBackupEnumDisabled
		backup.GeoRedundantBackup = &disabledEnum
	} else if azCl.IsMirrorRestore() {
		resourceGroup = azCl.MirrorResourceGroup()
		mirrorSubnet := azCl.MirrorSubnet()
		subnet = &mirrorSubnet
		if len(azCl.MirrorPrivateDNSZone()) != 0 {
			logger.Info(fmt.Sprintf("mirror enabled and dnsZone is specified, "+
				"overriding it to: %s", azCl.MirrorPrivateDNSZone()))
			privateDnsZone = to.Ptr(azCl.MirrorPrivateDNSZone())
		}
	}

	newServer := server.Server{
		Location: &locationName,
		Properties: &server.ServerProperties{
			AvailabilityZone:       avZone,
			Backup:                 backup,
			CreateMode:             &restoreType,
			PointInTimeUTC:         to.Ptr(func() time.Time { t, _ := time.Parse(time.RFC3339Nano, azCl.RestoreTime()); return t }()),
			SourceServerResourceID: to.Ptr(fmt.Sprintf("/subscriptions/%s/resourceGroups/%s/providers/Microsoft.DBforPostgreSQL/flexibleServers/%s", azCl.clientInfo(subscriptionId), azCl.ResourceGroup(), *oldServer.Name)),
			Network: &server.Network{
				DelegatedSubnetResourceID:   subnet,
				PrivateDNSZoneArmResourceID: privateDnsZone,
				PublicNetworkAccess:         oldServer.Properties.Network.PublicNetworkAccess,
			},
		},
	}

	poller, err := client.BeginCreate(ctx,
		resourceGroup,
		azCl.NewServerName(),
		newServer,
		nil)

	if err != nil {
		logger.Error("cannot create new Flexible Postgres database", zap.Error(err))
		panic(err)
	}

	resCreate, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		panic(err)
	}

	_ = resCreate
}

func (azCl *Client) stopServer(ctx context.Context, client *server.ServersClient) {
	logger.Info(fmt.Sprintf("stopping the server %s", azCl.ServerName()))
	pollerStop, err := client.BeginStop(ctx,
		azCl.ResourceGroup(),
		azCl.ServerName(),
		nil)
	if err != nil {
		logger.Warn(fmt.Sprintf("error during \"Stop server\" operation starting for the server %s", azCl.ServerName()), zap.Error(err))
		return
	}

	resStop, err := pollerStop.PollUntilDone(ctx, nil)
	if err != nil {
		logger.Warn(fmt.Sprintf("cannot stop the server %s", azCl.ServerName()), zap.Error(err))
		return
	}
	_ = resStop
}

func checkErrorStatus(status Status) {
	if r := recover(); r != nil {
		status.Status = statusFailed
		setStatus(status)
		panic(r)
	}
}

func setStatus(status Status) {
	statusFilePath := fmt.Sprintf("%s/%s", status.StatusFolder, status.RestoreId)
	err := util.WriteFile(statusFilePath, status)
	if err != nil {
		logger.Error("Cannot write to status file", zap.Error(err))
	}

	setCurrentStatus(status)
}

// TODO expire
func setCurrentStatus(status Status) {
	statusPath := fmt.Sprintf("%s/%s", status.StatusFolder, currentStatusFile)
	if status.Status == statusInProgress {
		err := util.WriteFile(statusPath, status)
		if err != nil {
			logger.Error("Cannot write to status file", zap.Error(err))
		}
	} else {
		util.DeleteFile(statusPath)
	}
}
