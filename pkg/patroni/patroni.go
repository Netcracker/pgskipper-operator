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

package patroni

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"github.com/google/go-cmp/cmp"

	yaml "gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"

	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/Netcracker/pgskipper-operator/pkg/util/constants"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	logger = util.GetLogger()
)

type ClusterResponse struct {
	Members []Members `json:"members"`
}

type Members map[string]interface{}

func SetWalArchiving(spec qubershipv1.PatroniServicesSpec, patroniUrl string) error {
	postgreSQLParams := map[string]interface{}{}
	recoveryParams := map[string]string{}
	if spec.PgBackRest != nil {
		logger.Info("pgBackRest feature is turned On, settings archive_command and restore_command for pgBackRest option")
		postgreSQLParams["archive_mode"] = constants.ArchiveModeOn
		postgreSQLParams["archive_command"] = constants.PgBackRestArchiveCommand
		recoveryParams["restore_command"] = constants.PgBackRestRestoreCommand
		postgreSQLParams["create_replica_methods"] = []string{"pgbackrest"}
	} else if spec.BackupDaemon.WalArchiving {
		logger.Info("WAL Archiving is turned On, settings archive_command and restore_command")
		postgreSQLParams["archive_mode"] = constants.ArchiveModeOn
		postgreSQLParams["archive_command"] = constants.ArchiveCommand
		recoveryParams["restore_command"] = constants.RestoreCommand
	} else {
		logger.Info("WAL Archiving is turned Off, omitting archive_command and restore_command")
		postgreSQLParams["archive_mode"] = constants.ArchiveModeOff
		postgreSQLParams["archive_command"] = ""
		recoveryParams["restore_command"] = ""
	}

	patchData := map[string]interface{}{
		"postgresql": map[string]interface{}{
			"parameters":    postgreSQLParams,
			"recovery_conf": recoveryParams,
		},
	}
	if spec.PgBackRest != nil {
		patchData = map[string]interface{}{
			"postgresql": map[string]interface{}{
				"parameters":    postgreSQLParams,
				"recovery_conf": recoveryParams,
				"pgbackrest": map[string]string{
					"command":   "pgbackrest --stanza=patroni --log-level-file=detail restore",
					"keep_data": "true",
					"no_params": "true",
				},
			},
		}
	}
	// send update to patroni
	if err := UpdatePatroniConfig(patchData, patroniUrl); err != nil {
		logger.Error("Failed to patch postgresql params via patroni", zap.Error(err))
		return err
	}
	return nil
}

func AddStandbyClusterSettings(cr *patroniv1.PatroniCore, configMap *corev1.ConfigMap, configMapKey string) {
	logger.Info("Apply standby cluster configuration in patroni template config map")
	standbyClusterConfiguration := getStandbyClusterConfiguration(cr)
	updateStandbyClusterSettings(configMap, standbyClusterConfiguration, configMapKey)
}

func DeleteStandbyClusterSettings(configMap *corev1.ConfigMap, configMapKey string) {
	logger.Info("Delete standby cluster configuration in patroni template config map")
	updateStandbyClusterSettings(configMap, "", configMapKey)
}

func ClearStandbyClusterConfigurationConfigMap(patroniUrl string) error {
	logger.Info("Clear standby cluster configuration in patroni config map")
	emptyStandbyClusterParm := map[string]interface{}{
		"standby_cluster": "",
	}
	if err := UpdatePatroniConfig(emptyStandbyClusterParm, patroniUrl); err != nil {
		logger.Error("Failed to update patroni config, exiting", zap.Error(err))
		return err
	}
	return nil
}

func AddStandbyClusterConfigurationConfigMap(cr *patroniv1.PatroniCore, patroniUrl string) error {
	logger.Info("Add standby cluster configuration in patroni config map")
	standbyClusterParm := map[string]interface{}{
		"standby_cluster": getStandbyClusterConfiguration(cr),
	}
	if err := UpdatePatroniConfig(standbyClusterParm, patroniUrl); err != nil {
		logger.Error("Failed to update patroni config with standby cluster configuration, exiting", zap.Error(err))
		return err
	}
	return nil
}

func SetSslStatus(cr *patroniv1.PatroniCore, patroniUrl string) error {
	postgreSQLParams := map[string]string{}
	logger.Info("Adding ssl configuration to patroni config")
	if cr.Spec.Tls != nil && !cr.Spec.Tls.Enabled {
		postgreSQLParams["ssl"] = "off"
		patchData := map[string]interface{}{
			"postgresql": map[string]interface{}{
				"parameters": postgreSQLParams,
			},
		}
		if err := UpdatePatroniConfig(patchData, patroniUrl); err != nil {
			logger.Error("Failed to update patroni config with ssl configuration, exiting", zap.Error(err))
			return err
		}
	}
	return nil
}

func GetPatroniCurrentConfig(patroniUrl string) (map[string]interface{}, error) {

	resp, err := http.Get(patroniUrl + "/config")
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve patroni config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d while retrieving patroni config", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(body, &config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal patroni config: %w", err)
	}

	return config, nil
}

func SetLdapConfig(cr *patroniv1.PatroniCore, patroniUrl string) error {
	logger.Info("Adding LDAP configuration to Patroni config")

	// Generate the LDAP configuration string
	ldapConfig := GenerateLDAPConfig(cr)

	// Retrieve existing Patroni configuration
	currentConfig, err := GetPatroniCurrentConfig(patroniUrl)
	if err != nil {
		logger.Error("Failed to retrieve current Patroni configuration", zap.Error(err))
		return err
	}

	// Retrieve the existing pg_hba entries
	currentPgHBA, ok := currentConfig["postgresql"].(map[string]interface{})["pg_hba"].([]interface{})
	if !ok {
		logger.Error("Failed to parse current pg_hba entries")
		return fmt.Errorf("invalid format for pg_hba entries")
	}

	// Convert currentPgHBA to a slice of strings
	var pgHBAEntries []string
	for _, entry := range currentPgHBA {
		if str, ok := entry.(string); ok {
			pgHBAEntries = append(pgHBAEntries, str)
		}
	}

	// Find the index of the first 'md5' entry in pg_hba to insert LDAP rules before it
	var md5Index int
	for i, entry := range pgHBAEntries {
		if strings.Contains(entry, "md5") {
			md5Index = i
			break
		}
	}

	// Insert LDAP config entries right before the first 'md5' entry so as to prevent failure of reconciliation cycle
	pgHBAEntries = append(pgHBAEntries[:md5Index], append(ldapConfig, pgHBAEntries[md5Index:]...)...)

	// Convert back pgHBAEntries to []interface{} to match the format of the original pg_hba
	var updatedPgHBA []interface{}
	for _, entry := range pgHBAEntries {
		updatedPgHBA = append(updatedPgHBA, entry)
	}

	// Update patchData with the modified pg_hba entries
	patchData := map[string]interface{}{
		"postgresql": map[string]interface{}{
			"pg_hba": updatedPgHBA,
		},
	}

	// Updating Patroni configuration
	if err := UpdatePatroniConfig(patchData, patroniUrl); err != nil {
		logger.Error("Failed to update Patroni config with LDAP configuration, exiting", zap.Error(err))
		return err
	}

	return nil
}

func IsStandbyClusterConfigurationExist(cr *patroniv1.PatroniCore) bool {
	standbyCluster := cr.Spec.Patroni.StandbyCluster
	emptyStandbyCluster := &patroniv1.StandbyCluster{}
	// return true if standby cluster is not empty
	return standbyCluster != nil && !cmp.Equal(emptyStandbyCluster, standbyCluster)
}

func IsPatroniHasDisabledStatus(cr *qubershipv1.PatroniServices) bool {
	smStatus := cr.Status.SiteManagerStatus
	return smStatus.Mode == "disabled" && smStatus.Status == "done"
}

func getStandbyClusterConfiguration(cr *patroniv1.PatroniCore) map[string]interface{} {
	standbyCluster := cr.Spec.Patroni.StandbyCluster
	standbyClusterConfiguration := map[string]interface{}{
		"host":                   standbyCluster.Host,
		"port":                   standbyCluster.Port,
		"primary_slot_name":      util.GetPatroniClusterName(cr.Spec.Patroni.ClusterName),
		"create_replica_methods": []string{"basebackup"},
	}
	return standbyClusterConfiguration
}

func updateStandbyClusterSettings(configMap *corev1.ConfigMap, settings interface{}, configMapKey string) *corev1.ConfigMap {

	var config map[string]interface{}
	err := yaml.Unmarshal([]byte(configMap.Data[configMapKey]), &config)
	if err != nil {
		logger.Error("Could not unmarshal patroni config map", zap.Error(err))
	}
	config["bootstrap"].(map[interface{}]interface{})["dcs"].(map[interface{}]interface{})["standby_cluster"] = settings
	result, err := yaml.Marshal(config)
	if err != nil {
		logger.Error("Could not marshal patroni config map", zap.Error(err))
	}
	configMap.Data[configMapKey] = string(result)
	return configMap
}

func UpdatePostgreSQLParams(patroni *patroniv1.Patroni, patroniUrl string) error {
	postgreSQLParams := map[string]interface{}{}
	for _, param := range patroni.PostgreSQLParams {
		param = strings.Replace(param, "=", ":", 1)
		splittedParam := strings.Split(param, ":")
		value := strings.Replace(param, splittedParam[0]+":", "", 1)
		splittedParam[0] = strings.TrimSpace(splittedParam[0])
		value = strings.TrimSpace(value)
		postgreSQLParams[splittedParam[0]] = value
	}

	if _, isMapContainsKey := postgreSQLParams["password_encryption"]; isMapContainsKey {
		logger.Info("Password encryption property set by CR")
	} else {
		postgreSQLParams["password_encryption"] = constants.PasswordEncryption
	}

	postgreSQL := map[string]interface{}{}

	if len(postgreSQLParams) > 0 {
		postgreSQL["parameters"] = postgreSQLParams
	}
	postgreSQL["pg_hba"] = getPgHba(patroni.PgHba)

	patchData := map[string]interface{}{
		"postgresql": postgreSQL,
	}

	if err := UpdatePatroniConfig(patchData, patroniUrl); err != nil {
		logger.Error("Failed to patch postgresql params via patroni", zap.Error(err))
		return err
	}
	return nil
}

func UpdatePatroniParams(patroni *patroniv1.Patroni, patroniUrl string) error {
	patroniLParams := map[string]interface{}{}
	for _, param := range patroni.PatroniParams {
		param = strings.Replace(param, "=", ":", 1)
		splittedParam := strings.Split(param, ":")
		splittedParam[0] = strings.TrimSpace(splittedParam[0])
		splittedParam[1] = strings.TrimSpace(splittedParam[1])
		patroniLParams[splittedParam[0]] = splittedParam[1]
	}

	if err := UpdatePatroniConfig(patroniLParams, patroniUrl); err != nil {
		logger.Error("Failed to patch patroni params via patroni", zap.Error(err))
		return err
	}
	return nil
}

func getPgHba(newHba []string) []string {
	if len(newHba) > 0 {
		return append(newHba, constants.PgHba...)
	}
	return constants.PgHba
}

func UpdatePatroniConfig(values map[string]interface{}, patroniUrl string) error {
	logger.Info("Will try to update PostgreSQL parameters via Patroni REST API")
	logger.Info(fmt.Sprintf("Patch body: %s", values))

	jsonValue, _ := json.Marshal(values)
	client := &http.Client{}
	if retryError := wait.PollUntilContextTimeout(context.Background(), time.Second, 1*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		req, err := http.NewRequest(http.MethodPatch, patroniUrl+"config", bytes.NewBuffer(jsonValue))
		if err != nil {
			logger.Error("Cannot prepare request for updating patroni", zap.Error(err))
			return false, nil
		}
		resp, err := client.Do(req)
		if err != nil {
			logger.Error(fmt.Sprintf("Failed to patch postgresql params via patroni, %v, Retrying", resp), zap.Error(err))
			return false, nil
		}
		return true, nil
	}); retryError != nil {
		logger.Error("Number of retries exceeded, giving up", zap.Error(retryError))
		return retryError
	}

	patroniHosts, err := getPatroniHosts(patroniUrl)
	if err != nil {
		return err
	}
	logger.Info(fmt.Sprintf("Patroni nodes %v", patroniHosts))
	for _, host := range patroniHosts {
		if err = restartIfPending(host); err != nil {
			logger.Error("Check if restart is required failed", zap.Error(err))
			return err
		}
	}

	return nil
}

func getPatroniHosts(patroniUrl string) ([]string, error) {
	hosts := make([]string, 0, 2)
	response := ClusterResponse{}
	if retryError := wait.PollUntilContextTimeout(context.Background(), time.Second, 1*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		resp, err := http.Get(patroniUrl + "cluster")
		if err != nil {
			logger.Error(fmt.Sprintf("cannot receive patroni hosts, get resp %v", resp), zap.Error(err))
			return false, nil
		}
		if resp.StatusCode == http.StatusOK {
			if err = json.NewDecoder(resp.Body).Decode(&response); err != nil {
				logger.Error("Obtain patroni hosts: response decode failed", zap.Error(err))
				return false, nil
			}
			for _, m := range response.Members {
				host, ok := m["host"]
				if !ok {
					hosts = make([]string, 0, 2)
					return false, nil
				}
				url := fmt.Sprintf("http://%s:8008/", host)
				hosts = append(hosts, url)
			}
			return len(hosts) > 0, nil
		}
		return false, nil
	}); retryError != nil {
		logger.Error("Number of retries exceeded, giving up", zap.Error(retryError))
		return nil, retryError
	}

	return hosts, nil
}

func restartIfPending(patroniUrl string) error {
	time.Sleep(15 * time.Second)
	return wait.PollUntilContextTimeout(context.Background(), 10*time.Second, 120*time.Minute, true, func(ctx context.Context) (done bool, err error) {
		resp, err := http.Get(patroniUrl + "patroni")
		if err != nil {
			logger.Error("Get request to patroni failed, retrying", zap.Error(err))
			return false, nil
		}
		defer func() {
			_ = resp.Body.Close()
		}()
		if resp.StatusCode != http.StatusOK {
			logger.Info(fmt.Sprintf("retrying, statusCode: %d, status: %s, retrying", resp.StatusCode, resp.Status))
			return false, nil
		}
		responseAsJson := map[string]interface{}{}
		if err = json.NewDecoder(resp.Body).Decode(&responseAsJson); err != nil {
			logger.Error("Check if restart is required: response decode failed", zap.Error(err))
			return false, nil
		} else {
			logger.Info("Check if restart is required: proceeding with pending_restart check")
			pendingRestart, ok := responseAsJson["pending_restart"]
			if ok && pendingRestart.(bool) {
				logger.Info("restartPending, will schedule restart of patroni")
				resp, err = http.Post(patroniUrl+"restart", "", nil)
				defer func() {
					_ = resp.Body.Close()
				}()
				if err == nil && resp.StatusCode == http.StatusOK {
					logger.Info("restart successful, exiting")
					return true, nil
				}
			} else {
				logger.Info("Check if restart is required: pending_restart is empty")
				return true, nil
			}
			return false, nil
		}
	})
}

func AddEtcdSettings(cr *patroniv1.PatroniCore, configMap *corev1.ConfigMap, configMapKey string) {
	logger.Info("Apply standby cluster configuration in patroni template config map")
	EtcdConfiguration := getEtcdConfiguration(cr)
	UpdatePatroniConfigMap(configMap, EtcdConfiguration, cr.Spec.Patroni.Dcs.Type, configMapKey)
}

func AddTagsSettings(cr *patroniv1.PatroniCore, configMap *corev1.ConfigMap, configMapKey string) {
	logger.Info("Apply standby cluster configuration in patroni template config map")
	tagsConfiguration := getTagsConfiguration(cr)
	UpdatePatroniConfigMap(configMap, tagsConfiguration, "tags", configMapKey)

}

func getEtcdConfiguration(cr *patroniv1.PatroniCore) map[string]interface{} {
	etcdClusterConfiguration := map[string]interface{}{
		"hosts": cr.Spec.Patroni.Dcs.Hosts,
	}
	return etcdClusterConfiguration
}

func getTagsConfiguration(cr *patroniv1.PatroniCore) map[string]string {
	tagsConfiguration := cr.Spec.Patroni.Tags
	return tagsConfiguration
}

func UpdatePatroniConfigMap(configMap *corev1.ConfigMap, settings interface{}, key string, configMapKey string) *corev1.ConfigMap {
	var config map[string]interface{}
	err := yaml.Unmarshal([]byte(configMap.Data[configMapKey]), &config)
	if err != nil {
		logger.Error("Could not unmarshal patroni config map", zap.Error(err))
	}
	delete(config, "kubernetes")

	config[key] = settings
	result, err := yaml.Marshal(config)
	if err != nil {
		logger.Error("Could not marshal patroni config map", zap.Error(err))
	}
	configMap.Data[configMapKey] = string(result)
	return configMap
}

func GenerateLDAPConfig(cr *patroniv1.PatroniCore) []string {

	ldapServer := cr.Spec.Ldap.Server
	ldapPort := cr.Spec.Ldap.Port
	ldapBasedn := cr.Spec.Ldap.BaseDN
	ldapBinddn := cr.Spec.Ldap.BindDN
	ldapBindpasswd := cr.Spec.Ldap.BindPasswd
	ldapSearchAttribute := cr.Spec.Ldap.LdapSearchAttr

	return []string{
		fmt.Sprintf(
			"host all +pgadminrole 0.0.0.0/0 ldap ldapserver=%s ldapport=%v ldapbasedn=\"%s\" ldapbinddn=\"%s\" ldapbindpasswd=\"%s\" ldapsearchattribute=\"%s\"",
			ldapServer, ldapPort, ldapBasedn, ldapBinddn, ldapBindpasswd, ldapSearchAttribute,
		),
	}
}
