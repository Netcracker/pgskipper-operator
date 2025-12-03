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

package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Netcracker/pgskipper-backup-daemon/pkg/k8s"
	"github.com/Netcracker/pgskipper-backup-daemon/pkg/util"
)

const (
	restoreConfigCM = "external-restore-config"

	MirrorSubnetKey      = "mirror.subnet"
	MirrorPrivateDNSZone = "mirror.privateDnsZone"
	MirrorRGKey          = "mirror.resourceGroup"
	GeoSubnetKey         = "geo.subnet"
	GeoLocationKey       = "geo.location"
	GeoRGKey             = "geo.resourceGroup"
	GeoPrivateDNSZone    = "geo.privateDnsZone"
	GeoHaKey             = "geo.ha"
	MainLocationKey      = "main.location"
	MainRGKey            = "main.resourceGroup"
)

var (
	logger = util.Logger
)

// RestoreConfig holds configuration for restore
// if subnet is specified, means mirror restore -> don't stop source, add additional tag, override subnet from CM
// restoreAsSeparate -> restore az pg as separe instance -> don't stop source, don't update k8s service
type RestoreConfig struct {
	serverName        string
	newServerName     string
	restoreId         string
	restoreTime       string
	statusFolder      string
	restoreAsSeparate string
	geoRestore        string
	mirror            string
	cmData            map[string]string
}

func (cfg *RestoreConfig) NewServerName() string {
	return cfg.newServerName
}

func (cfg *RestoreConfig) ServerName() string {
	return cfg.serverName
}

func (cfg *RestoreConfig) RestoreAsSeparate() bool {
	return strings.Compare(strings.ToLower(cfg.restoreAsSeparate), "true") == 0
}

func (cfg *RestoreConfig) IsMirrorRestore() bool {
	return strings.ToLower(cfg.mirror) == "true"
}

func (cfg *RestoreConfig) MirrorResourceGroup() string {
	return cfg.cmData[MirrorRGKey]
}

func (cfg *RestoreConfig) MirrorSubnet() string {
	return cfg.cmData[MirrorSubnetKey]
}

func (cfg *RestoreConfig) MirrorPrivateDNSZone() string {
	return cfg.cmData[MirrorPrivateDNSZone]
}

func (cfg *RestoreConfig) ResourceGroup() string {
	return cfg.cmData[MainRGKey]
}

func (cfg *RestoreConfig) Location() string {
	return cfg.cmData[MainLocationKey]
}

func (cfg *RestoreConfig) IsGeoRestore() bool {
	return strings.ToLower(cfg.geoRestore) == "true"
}

func (cfg *RestoreConfig) GeoSubnet() string {
	return cfg.cmData[GeoSubnetKey]
}

func (cfg *RestoreConfig) GeoHA() string {
	return cfg.cmData[GeoHaKey]
}

func (cfg *RestoreConfig) GeoLocation() string {
	return cfg.cmData[GeoLocationKey]
}

func (cfg *RestoreConfig) GeoResourceGroup() string {
	return cfg.cmData[GeoRGKey]
}

func (cfg *RestoreConfig) GeoPrivateDNSZone() string {
	return cfg.cmData[GeoPrivateDNSZone]
}

func (cfg *RestoreConfig) RestoreID() string {
	return cfg.restoreId
}

func (cfg *RestoreConfig) StatusFolder() string {
	return cfg.statusFolder
}

func (cfg *RestoreConfig) RestoreTime() string {
	return cfg.restoreTime
}

// StopSource Stop of Source should be done only in regular restore
// restoreAsSeparate=false subnet=false
func (cfg *RestoreConfig) StopSource() bool {
	return !cfg.RestoreAsSeparate() && !cfg.IsMirrorRestore() && !cfg.IsGeoRestore()
}

func NewRestoreConfig(restoreId, restoreTime, statusFolder, restoreAsSeparate, geoRestore, subnet string) *RestoreConfig {
	serverName, err := k8s.GetServerName()
	if err != nil {
		panic(err)
	}
	newServerName := GenerateNewName(serverName)

	var cmData map[string]string
	cm, err := k8s.GetCM(restoreConfigCM)
	if err != nil {
		panic(err)
	}
	cmData = cm.Data

	logger.Info(fmt.Sprintf("restoreAsSeparate: %s, subnet: %s, geoRestore %s", restoreAsSeparate, subnet, geoRestore))
	return &RestoreConfig{
		serverName:        serverName,
		newServerName:     newServerName,
		restoreId:         restoreId,
		restoreTime:       restoreTime,
		statusFolder:      statusFolder,
		restoreAsSeparate: restoreAsSeparate,
		geoRestore:        geoRestore,
		mirror:            subnet,
		cmData:            cmData,
	}
}

func GenerateNewName(oldServerName string) string {
	now := time.Now()
	timeString := strconv.Itoa(int(now.Unix()))
	if strings.HasSuffix(oldServerName, "-restored") {
		oldServerSlice := strings.Split(oldServerName, "-")
		oldServerSlice[len(oldServerSlice)-2] = timeString
		return strings.Join(oldServerSlice, "-")
	}
	return fmt.Sprintf("%s-%s-restored", oldServerName, timeString)
}
