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

package backup

import (
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/cluster"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

type BackupAdapter struct {
	ClusterAdapter cluster.ClusterAdapter
	DaemonAddress  string
	Keep           string
	Auth           bool
	User           string
	Password       string
	PgSSl          string
	Client         *fasthttp.Client
	log            *zap.Logger
}

type PostgresDaemonBackupRequest struct {
	Databases []string `json:"databases"`
	Keep      string   `json:"keep"`
}

type PostgresDaemonBackup struct {
	BackupId string `json:"backupId"`
}

type PostgresBackupStatus struct {
	BackupId  string                                  `json:"backupId"`
	Status    string                                  `json:"status"`
	Databases map[string]PostgresDatabaseBackupStatus `json:"databases"`
}

type PostgresDatabaseBackupStatus struct {
	Status string `json:"status"`
}

type PostgresDaemonRestoreRequest struct {
	BackupId          string            `json:"backupId"`
	Databases         []string          `json:"databases"`
	DatabasesMapping  map[string]string `json:"databasesMapping"`
	Force             bool              `json:"force"`
	RestoreRoles      string            `json:"restoreRoles"`
	SingleTransaction string            `json:"singleTransaction"`
	DbaasClone        bool              `json:"dbaasClone"`
}

type PostgresRestoreResponse struct {
	TrackingId string `json:"trackingId"`
}

type PostgresRestoreStatus struct {
	TrackingId string `json:"trackingId"`
	Status     string `json:"status"`
}

type PostgresBackupDeleteResponse struct {
	BackupId string `json:"backupId"`
	Status   string `json:"status"`
	Message  string `json:"message"`
}

type BackupDaemonResponse struct {
	Status int
	Body   []byte
}
