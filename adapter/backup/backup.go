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
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/cluster"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	coreUtils "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	uuid "github.com/google/uuid"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"
)

const (
	Post = "POST"
	Get  = "GET"
)

var generatorMutex = sync.Mutex{}

func newPostgresAdapterBackupActionTrack(task *PostgresBackupStatus) dao.DatabaseAdapterBaseTrack {
	var details *dao.DatabasesBackupAdapt = nil

	if task.Status == "Successful" {
		details = &dao.DatabasesBackupAdapt{
			LocalId: task.BackupId,
		}
	}

	return dao.DatabaseAdapterBaseTrack{
		Status:  mapStatus(task.Status),
		TrackId: task.BackupId,
		Action:  "BACKUP",
		Details: details,
	}
}

func newPostgresAdapterRestoreActionTrack(task *PostgresRestoreStatus) dao.DatabaseAdapterRestoreTrack {
	return dao.DatabaseAdapterRestoreTrack{
		DatabaseAdapterBaseTrack: dao.DatabaseAdapterBaseTrack{
			Status:  mapStatus(task.Status),
			TrackId: task.TrackingId,
			Action:  "RESTORE",
		},
	}
}

func newPostgresAdapterRestoreActionTrackWithRequest(task *PostgresRestoreResponse, req PostgresDaemonRestoreRequest) *dao.DatabaseAdapterRestoreTrack {
	if req.DatabasesMapping != nil {
		return &dao.DatabaseAdapterRestoreTrack{
			DatabaseAdapterBaseTrack: dao.DatabaseAdapterBaseTrack{
				Status:  "PROCEEDING",
				TrackId: task.TrackingId,
				Action:  "RESTORE",
			},
			ChangedNameDb: req.DatabasesMapping,
		}
	}

	return &dao.DatabaseAdapterRestoreTrack{
		DatabaseAdapterBaseTrack: dao.DatabaseAdapterBaseTrack{
			Status:  "PROCEEDING",
			TrackId: task.TrackingId,
			Action:  "RESTORE",
		},
	}
}

func certFileExists() bool {
	path := "/certs/tls.crt"
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

func NewServiceAdapter(clusterAdapter cluster.ClusterAdapter, backupAddress string, keep string, auth bool, user string, password string, pgSSl string) *BackupAdapter {
	httpClient := &fasthttp.Client{}
	if pgSSl == "on" && certFileExists() {
		httpClient.TLSConfig = setTLSConfig()
	}

	client := &BackupAdapter{
		ClusterAdapter: clusterAdapter,
		DaemonAddress:  backupAddress,
		Keep:           keep,
		Auth:           auth,
		User:           user,
		Password:       password,
		PgSSl:          pgSSl,
		Client:         httpClient,
		log:            util.GetLogger(),
	}
	return client
}

func setTLSConfig() *tls.Config {
	cert, err := tls.LoadX509KeyPair("/certs/tls.crt", "/certs/tls.key")
	if err != nil {
		panic(fmt.Sprintf("Error during load a key pair %v", zap.Error(err)))
	}

	// Load CA cert
	caCert, err := os.ReadFile("/certs/ca.crt")
	if err != nil {
		panic(fmt.Sprintf("Error during load a key pair %v", zap.Error(err)))
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	// Setup HTTPS client
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
	return tlsConfig
}
func (ba BackupAdapter) sendRequest(ctx context.Context, method string, path string, body interface{}) (*BackupDaemonResponse, error) {
	logger := util.ContextLogger(ctx)
	url := ba.getUrl(path)
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)
	res := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(res)

	req.Header.SetMethod(method)
	req.SetRequestURI(url)

	if ba.Auth {
		auth := ba.User + ":" + ba.Password
		authHeaderValue := base64.StdEncoding.EncodeToString([]byte(auth))
		req.Header.Set("Authorization", authHeaderValue)
	}

	if body != nil {
		var requestBody []byte
		req.Header.Set("Accept", "application/json")
		req.Header.SetContentType("application/json")
		requestBody, err := json.Marshal(body)
		if err != nil {
			logger.Error(fmt.Sprintf("Error during marshal body: %+v", body), zap.Error(err))
			return nil, err
		}
		req.SetBody(requestBody)
	}

	if err := ba.Client.Do(req, res); err != nil {
		logger.Error(fmt.Sprintf("Error during send %s request %s", method, ba.getUrl("/backup/request")), zap.Error(err))
		return nil, err
	}

	responseBody := res.Body()
	responseBodyCopy := make([]byte, len(responseBody))
	copy(responseBodyCopy, responseBody)

	return &BackupDaemonResponse{Status: res.StatusCode(), Body: responseBodyCopy}, nil
}

func (ba BackupAdapter) CollectBackup(ctx context.Context, databases []string, keepFromRequest string, allowEviction bool) dao.DatabaseAdapterBaseTrack {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Send request to collect backup to %s", ba.DaemonAddress))
	keep := "forever"

	if allowEviction {
		if keepFromRequest != "" {
			keep = keepFromRequest
		} else {
			keep = ba.Keep
		}
	}

	requestBody := PostgresDaemonBackupRequest{
		Databases: databases,
		Keep:      keep,
	}

	response, err := ba.sendRequest(ctx, Post, "/backup/request", requestBody)
	if err != nil {
		panic(err)
	}

	var postgresDaemonBackup PostgresDaemonBackup
	err = json.Unmarshal(response.Body, &postgresDaemonBackup)
	if err != nil {
		logger.Error("Error during unmarshal response from /backup/request", zap.Error(err))
		panic(err)
	}

	trackBackup, _ := ba.TrackBackup(ctx, postgresDaemonBackup.BackupId)

	return trackBackup
}

func (ba BackupAdapter) TrackRestore(ctx context.Context, trackId string) (dao.DatabaseAdapterRestoreTrack, bool) {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Request status information for restore %s to daemon %s", trackId, ba.DaemonAddress))
	status, found := ba.getRestoreStatus(ctx, trackId)
	if !found {
		return dao.DatabaseAdapterRestoreTrack{}, false
	}

	return newPostgresAdapterRestoreActionTrack(status), true
}

func (ba BackupAdapter) TrackBackup(ctx context.Context, trackId string) (dao.DatabaseAdapterBaseTrack, bool) {
	logger := util.ContextLogger(ctx)
	logger.Info(fmt.Sprintf("Request status information for backup %s to daemon %s", trackId, ba.DaemonAddress))
	status, found := ba.getBackupStatus(ctx, trackId)
	if !found {
		return dao.DatabaseAdapterBaseTrack{}, false
	}

	return newPostgresAdapterBackupActionTrack(status), true
}

func (ba BackupAdapter) getBackupStatus(ctx context.Context, trackId string) (*PostgresBackupStatus, bool) {
	response, err := ba.sendRequest(ctx, Get, "/backup/status/"+trackId, nil)
	if err != nil {
		panic(err)
	}
	if response.Status == http.StatusNotFound {
		return nil, false
	}

	var backupStatus PostgresBackupStatus
	err = json.Unmarshal(response.Body, &backupStatus)
	if err != nil {
		util.ContextLogger(ctx).Error("Error during unmarshal response from /backup/status", zap.Error(err))
		panic(err)
	}

	return &backupStatus, true
}

func (ba BackupAdapter) getRestoreStatus(ctx context.Context, trackId string) (*PostgresRestoreStatus, bool) {
	logger := util.ContextLogger(ctx)
	response, err := ba.sendRequest(ctx, Get, "/restore/status/"+trackId, nil)
	if err != nil {
		panic(err)
	}
	if response.Status == http.StatusNotFound {
		return nil, false
	}

	var restoreStatus PostgresRestoreStatus
	err = json.Unmarshal(response.Body, &restoreStatus)
	if err != nil {
		logger.Error("Error during unmarshal response from /restore/request", zap.Error(err))
		panic(err)
	}

	return &restoreStatus, true
}

func (ba BackupAdapter) EvictBackup(ctx context.Context, backupId string) (string, bool) {
	logger := util.ContextLogger(ctx)
	response, err := ba.sendRequest(ctx, Post, "/delete/"+backupId, nil)
	if err != nil {
		panic(err)
	}
	if response.Status == http.StatusNotFound {
		return "", false
	}
	var deleteResponse PostgresBackupDeleteResponse
	err = json.Unmarshal(response.Body, &deleteResponse)
	if err != nil {
		logger.Error("Error during parse delete response", zap.Error(err))
		panic(err)
	}

	var status = "Unknown"
	if deleteResponse.Status != "" {
		if deleteResponse.Status == "Successful" {
			status = "SUCCESS"
		} else {
			status = "FAIL"
		}
	}
	//todo
	return status, true

}

func (ba BackupAdapter) RestoreBackup(ctx context.Context, backupId string, databases []dao.DbInfo, regenerateNames bool, oldNameFormat bool) (*dao.DatabaseAdapterRestoreTrack, error) {
	logger := util.ContextLogger(ctx)
	restoreRoles := "true"
	dbaasClone := false
	var databaseMapping map[string]string
	if regenerateNames {
		restoreRoles = "false"
		dbaasClone = true
		databaseMapping = make(map[string]string, len(databases))

		for _, database := range databases {
			newDbName, err := generateNewDBName(ctx, database, oldNameFormat)
			if err != nil {
				return nil, err
			}
			databaseMapping[database.Name] = newDbName
		}
	}

	req := PostgresDaemonRestoreRequest{
		BackupId:          backupId,
		Databases:         getDbNames(databases),
		DatabasesMapping:  databaseMapping,
		Force:             true,
		RestoreRoles:      restoreRoles,
		SingleTransaction: "true",
		DbaasClone:        dbaasClone,
	}

	response, err := ba.sendRequest(ctx, Post, "/restore/request", req)
	if err != nil {
		return nil, err
	}

	var postgresRestoreResponse PostgresRestoreResponse
	err = json.Unmarshal(response.Body, &postgresRestoreResponse)
	if err != nil {
		logger.Error("Error during parse response from backupDaemon", zap.Error(err))
		return nil, err
	}

	return newPostgresAdapterRestoreActionTrackWithRequest(&postgresRestoreResponse, req), nil
}

func generateNewDBName(ctx context.Context, database dao.DbInfo, oldNameFormat bool) (newDbName string, err error) {
	generatorMutex.Lock()
	defer generatorMutex.Unlock()
	// perform sleep to avoid timestamp collision
	time.Sleep(1 * time.Millisecond)

	if oldNameFormat {
		newDbName = generateDbNameWithUUID(ctx, database.Name)
	} else if database.Prefix != nil {
		newDbName = coreUtils.RegenerateDbName(*database.Prefix, util.GetPgDBLength())
	} else {
		newDbName, err = coreUtils.PrepareDatabaseName(database.Namespace, database.Microservice, util.GetPgDBLength())
		if err != nil {
			return newDbName, err
		}
	}
	return newDbName, err
}

func getDbNames(dbInfo []dao.DbInfo) []string {
	result := make([]string, 0, len(dbInfo))
	for _, db := range dbInfo {
		result = append(result, db.Name)
	}
	return result
}

func generateDbNameWithUUID(ctx context.Context, dbName string) string {
	logger := util.ContextLogger(ctx)

	uuidName := uuid.New()
	newDbName := uuidName.String()
	newDbName = strings.ReplaceAll(newDbName, "-", "")

	maxLen := util.GetPgDBLength()
	prefixFromDb := dbName
	if len(dbName) > maxLen-len(newDbName)-1 {
		prefixFromDb = dbName[:maxLen-1-len(dbName)]
	}
	newDbName = fmt.Sprintf("%s_%s", prefixFromDb, uuidName.String())

	logger.Info(fmt.Sprintf("New name %s was generated for db with name %s", newDbName, dbName))
	return strings.ToLower(newDbName)
}

func (ba BackupAdapter) getUrl(route string) string {
	return ba.DaemonAddress + route
}

func mapStatus(daemonStatus string) dao.DatabaseAdapterBackupAdapterTrackStatus {
	switch daemonStatus {
	case "Planned":
		return "PROCEEDING"
	case "In progress":
		return "PROCEEDING"
	case "Failed":
		return "FAIL"
	case "Successful":
		return "SUCCESS"
	default:
		return "FAIL"
	}
}
