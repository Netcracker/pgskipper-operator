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

//nolint

package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/cluster"
	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/Netcracker/qubership-dbaas-adapter-core/pkg/dao"
	coreUtils "github.com/Netcracker/qubership-dbaas-adapter-core/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

var (
	host     = "test_host"
	user     = "test_user"
	port     = 1234
	password = "password"
)

type PostgresClusterAdapterMock struct {
	mock.Mock
	cluster.ClusterAdapter
}

func (ca *PostgresClusterAdapterMock) GetHost() string {
	return ca.Called().String(0)
}

func (ca *PostgresClusterAdapterMock) GetPort() int {
	return ca.Called().Int(0)
}

func (ca *PostgresClusterAdapterMock) GetUser() string {
	ca.Called()
	return ca.Called().String(0)
}

func (ca *PostgresClusterAdapterMock) GetPassword() string {
	ca.Called()
	return ca.Called().String(0)
}

func (ca *PostgresClusterAdapterMock) GetConnection(ctx context.Context) (cluster.Conn, error) {
	args := ca.Called()
	return args.Get(0).(cluster.Conn), args.Error(1)
}

type MockConn struct {
	mock.Mock
	cluster.Conn
}

type Request interface {
	URI() *url.URL
}

type Response interface {
	Body() []byte
}

func startTestServer() {
	// Handle requests on "/backup/request" and "/backup/status/{backupId}"
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Println("string url")
		fmt.Println(r.URL.Path)

		// Check if the URL path starts with "/backup/request"
		if strings.HasPrefix(r.URL.Path, "/backup/request") {

			response := PostgresDaemonBackup{
				BackupId: "20240105T0836",
			}

			jsonResponse, err := json.Marshal(response)
			fmt.Println("string json respnse")
			fmt.Println(string(jsonResponse))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(jsonResponse); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}

		// Check if the URL path starts with "/backup/status/"
		statusPrefix := "/backup/status/"
		if strings.HasPrefix(r.URL.Path, statusPrefix) {
			backupId := strings.TrimPrefix(r.URL.Path, statusPrefix)

			response := PostgresBackupStatus{
				BackupId: backupId,
				Status:   "Successful",
				Databases: map[string]PostgresDatabaseBackupStatus{
					"db1": {Status: "Successful"},
					"db2": {Status: "In progress"},
				},
			}

			jsonResponse, err := json.Marshal(response)
			fmt.Println("string json respnse")
			fmt.Println(string(jsonResponse))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(jsonResponse); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}

		//restore status
		restoreStatusPrefix := "/restore/status/"
		if strings.HasPrefix(r.URL.Path, restoreStatusPrefix) {
			trackId := strings.TrimPrefix(r.URL.Path, restoreStatusPrefix)

			response := PostgresRestoreStatus{
				TrackingId: trackId,
				Status:     "Successful",
			}

			jsonResponse, err := json.Marshal(response)
			fmt.Println("string json respnse")
			fmt.Println(string(jsonResponse))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(jsonResponse); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}

		//EVictBackup
		EvictBackupPrefix := "/delete/"
		if strings.HasPrefix(r.URL.Path, EvictBackupPrefix) {
			trackId := strings.TrimPrefix(r.URL.Path, EvictBackupPrefix)

			response := PostgresBackupDeleteResponse{
				BackupId: trackId,
				Status:   "Successful",
				Message:  "Backup successfully evicted.",
			}

			jsonResponse, err := json.Marshal(response)
			fmt.Println("string json respnse")
			fmt.Println(string(jsonResponse))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(jsonResponse); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}

		//RestoreRequest
		RestoreRequestPrefix := "/restore/request"
		if strings.HasPrefix(r.URL.Path, RestoreRequestPrefix) {
			trackId := "20240105T0836"

			response := PostgresRestoreResponse{
				TrackingId: trackId,
			}

			jsonResponse, err := json.Marshal(response)
			fmt.Println("string json respnse")
			fmt.Println(string(jsonResponse))
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(jsonResponse); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}

		http.Error(w, "Invalid Endpoint", http.StatusNotFound)

	})

	go func() {
		if err := http.ListenAndServe(":8080", nil); err != nil {
			panic(err)
		}
	}()

	time.Sleep(100 * time.Millisecond)

}

func init() {
	startTestServer()
}

func TestCollectBackup(t *testing.T) {

	ca := new(PostgresClusterAdapterMock)
	conn := new(MockConn)

	ca.On("GetConnection").Return(conn, nil)
	ca.On("GetHost").Return(host)
	ca.On("GetPort").Return(port)
	ca.On("GetUser").Return(user)
	ca.On("GetPassword").Return(password)

	databases := []string{"db1", "db2"}

	aggAddress := "http://localhost:8080"

	backupAdapater := NewServiceAdapter(ca, aggAddress, "forever", false, user, password, "off")

	keepFromRequest := "30 days"
	allowEviction := true

	trackBackup := backupAdapater.CollectBackup(context.Background(), databases, keepFromRequest, allowEviction)

	assert.Equal(t, trackBackup.Action, dao.DatabaseAdapterAction("BACKUP"))
	assert.Equal(t, trackBackup.TrackId, "20240105T0836")
	assert.Equal(t, trackBackup.Status, dao.DatabaseAdapterBackupAdapterTrackStatus("SUCCESS"))

}

func TestTrackBackupTest(t *testing.T) {

	aggAddress := "http://localhost:8080"

	statusRequestBody := PostgresBackupStatus{
		BackupId: "20240105T0836",
		Status:   "Successful",
		Databases: map[string]PostgresDatabaseBackupStatus{
			"db1": {Status: "Successful"},
			"db2": {Status: "In progress"},
		},
	}

	backupAdapater := NewServiceAdapter(nil, aggAddress, "", false, "", "", "")

	trackStatus := backupAdapater.TrackBackup(context.Background(), "20240105T0836")
	backupStatus := backupAdapater.getBackupStatus(context.Background(), "20240105T0836")
	BackupActionTrack := newPostgresAdapterBackupActionTrack(backupStatus)

	fmt.Printf("Backup new statysStatus: %v\n", trackStatus)

	assert.Equal(t, BackupActionTrack.TrackId, statusRequestBody.BackupId)
	assert.Equal(t, BackupActionTrack.Status, dao.DatabaseAdapterBackupAdapterTrackStatus("SUCCESS"))
	assert.Equal(t, BackupActionTrack.Action, dao.DatabaseAdapterAction("BACKUP"))

}

func TestTrackRestoreStatus(t *testing.T) {

	aggAddress := "http://localhost:8080"

	statusRequestBody := PostgresRestoreStatus{
		TrackingId: "20240105T0836",
		Status:     "SUCCESS",
	}

	backupAdapater := NewServiceAdapter(nil, aggAddress, "", false, "", "", "")

	backupStatus := backupAdapater.TrackRestore(context.Background(), "20240105T0836")

	backupRestoreStatus, err := backupAdapater.getRestoreStatus(context.Background(), "20240105T0836")
	assert.Nil(t, err)
	fmt.Printf("Backup restire Status small: %v\n", backupRestoreStatus)
	RestoreActionTrack := newPostgresAdapterRestoreActionTrack(backupRestoreStatus)
	fmt.Printf("Backup restire Status: %v\n", backupStatus)

	assert.Equal(t, RestoreActionTrack.TrackId, statusRequestBody.TrackingId)
	assert.Equal(t, RestoreActionTrack.Status, dao.DatabaseAdapterBackupAdapterTrackStatus("SUCCESS"))
	assert.Equal(t, RestoreActionTrack.Action, dao.DatabaseAdapterAction("RESTORE"))

}

func TestEvictBackupStatus(t *testing.T) {

	aggAddress := "http://localhost:8080"

	backupAdapater := NewServiceAdapter(nil, aggAddress, "", false, "", "", "")

	backupStatus := backupAdapater.EvictBackup(context.Background(), "20240105T0836")

	fmt.Printf("Backup EVICT NEwStatus: %v\n", backupStatus)
	assert.Equal(t, backupStatus, "SUCCESS")

}

func TestRestoreRequestStatus(t *testing.T) { // TODO: Check test logic

	databaseNames := []string{"db1", "db2"}
	databases := []dao.DbInfo{}
	var regenerateNames = true

	var databaseMapping map[string]string
	if regenerateNames {
		databaseMapping = make(map[string]string, len(databaseNames))

		for _, database := range databaseNames {
			databaseMapping[database] = coreUtils.RegenerateDbName("test", util.GetPgDBLength())
			databases = append(databases, dao.DbInfo{Name: database, Namespace: "testNamespace", Microservice: "testMicroservice"})
		}
	}

	req := PostgresDaemonRestoreRequest{
		BackupId:          "20240105T0836",
		Databases:         databaseNames,
		DatabasesMapping:  databaseMapping,
		Force:             true,
		RestoreRoles:      "true",
		SingleTransaction: "true",
	}

	aggAddress := "http://localhost:8080"

	statusResponseBody := PostgresRestoreResponse{
		TrackingId: "20240105T0836",
	}

	backupAdapater := NewServiceAdapter(nil, aggAddress, "", false, "", "", "")

	backupStatus, err := backupAdapater.RestoreBackup(context.Background(), "20240105T0836", databases, regenerateNames, false)
	if err != nil {
		panic(err)
	}

	fmt.Printf("Status response body: %v\n", statusResponseBody)
	RestoreActionWithRequest := newPostgresAdapterRestoreActionTrackWithRequest(&statusResponseBody, req)
	fmt.Printf("Backup Restore request NEwStatus: %v\n", backupStatus)

	assert.Equal(t, RestoreActionWithRequest.Status, dao.DatabaseAdapterBackupAdapterTrackStatus("PROCEEDING"))
	assert.Equal(t, RestoreActionWithRequest.Action, dao.DatabaseAdapterAction("RESTORE"))
	assert.NotNil(t, RestoreActionWithRequest.ChangedNameDb)
	assert.NotNil(t, RestoreActionWithRequest.TrackId)

}

func TestInvalidEndpointStatus(t *testing.T) {

	aggAddress := "http://localhost:8080"

	statusRequestBody := PostgresBackupDeleteResponse{
		BackupId: "20240105T0836",
		Status:   "Successful",
		Message:  "Backup successfully evicted.",
	}

	backupAdapater := NewServiceAdapter(nil, aggAddress, "", false, "", "", "")

	ResponseStatus, err := backupAdapater.sendRequest(context.Background(), "POST", "/postgres/", statusRequestBody)
	assert.Nil(t, err)

	str := asciiToStr(ResponseStatus)
	assert.Equal(t, str, "Invalid Endpoint\n")

}

func asciiToStr(asciiBytes []byte) string {
	return string(asciiBytes)
}
