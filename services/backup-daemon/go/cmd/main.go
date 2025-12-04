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

package main

import (
	"flag"

	"github.com/Netcracker/pgskipper-backup-daemon/pkg/config"
	"github.com/Netcracker/pgskipper-backup-daemon/pkg/util"

	"github.com/Netcracker/pgskipper-backup-daemon/pkg/azure"
	"github.com/Netcracker/pgskipper-backup-daemon/pkg/k8s"
)

var (
	restoreId         = flag.String("restore_id", "", "Id of restore operation")
	restoreTime       = flag.String("restore_time", "", "Time for restore database from")
	restoreFolder     = flag.String("restore_folder", "", "Folder to save restore statuses")
	restoreAsSeparate = flag.String("restore_as_separate", "false", "Flag to skip update external service")
	geoRestore        = flag.String("geo_restore", "false", "Flag to perform geo restore")
	subnet            = flag.String("subnet", "false", "override subnet for restored instance")
)

func main() {
	flag.Parse()
	restoreCfg := config.NewRestoreConfig(*restoreId, *restoreTime, *restoreFolder, *restoreAsSeparate, *geoRestore, *subnet)
	restoreClient := azure.NewRestoreClientWithRestoreConfig(restoreCfg)

	util.ConfigureAzLogging()

	restoreClient.RestoreDatabase()

	if restoreCfg.RestoreAsSeparate() {
		return
	}

	err := k8s.UpdateExternalService(restoreCfg.NewServerName())
	if err != nil {
		panic(err)
	}
	err = k8s.UpdateExternalCM(restoreCfg.ServerName(), restoreCfg.NewServerName())
	if err != nil {
		panic(err)
	}
}
