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

package vault

import (
	"context"
	"fmt"
	"sync"

	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"

	types "github.com/Netcracker/pgskipper-operator-core/api/v1"
	pgclient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/helper"
	"github.com/Netcracker/pgskipper-operator/pkg/util"
	"github.com/hashicorp/vault/api"
	"go.uber.org/zap"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	RotationPeriod = "175200h"
)

var (
	logger = util.GetLogger()
)

type Client struct {
	helper                     *helper.PatroniHelper
	k8sClient                  crclient.Client
	coreCr                     *patroniv1.PatroniCore
	isMetricCollectorInstalled bool
	isBackupDaemonInstalled    bool
	registration               *types.VaultRegistration
	mu                         sync.Mutex
}

func NewClient() *Client {
	newK8sClient, err := util.GetClient()
	if err != nil {
		panic(err)
	}
	return &Client{
		helper:    helper.GetPatroniHelper(),
		k8sClient: newK8sClient,
	}
}

func (c *Client) UpdateCr(kind string) {
	//Maybe not the best decision
	if kind == "PatroniServices" {
		cr, err := c.helper.GetPostgresServiceCR()
		if err != nil {
			logger.Error("Can't get PatroniServices CR for Vault client")
			panic(err)
		}
		c.registration = cr.Spec.VaultRegistration
		c.isMetricCollectorInstalled = cr.Spec.MetricCollector != nil
		c.isBackupDaemonInstalled = cr.Spec.BackupDaemon != nil
	} else {
		coreCr, err := c.helper.GetPatroniCoreCR()
		if err != nil {
			logger.Error("Can't get PatroniCore CR for Vault client")
			panic(err)
		}
		c.registration = coreCr.Spec.VaultRegistration
		c.coreCr = coreCr
	}
}

func (c *Client) CreatePostgresVaultRole(userName string) (string, error) {
	roleName := GetVaultRoleName(userName)
	logger.Info(fmt.Sprintf("Creation of vault role %s", roleName))
	token, err := util.ReadTokenFromFile()
	if err != nil {
		logger.Error("can not read the token from file")
		return "", err
	}
	client := c.vaultSetToken(token)

	data := map[string]interface{}{
		"username":            userName,
		"db_name":             c.registration.DbEngine.Name,
		"rotation_period":     RotationPeriod,
		"rotation_statements": []string{"ALTER USER \"{{name}}\" WITH PASSWORD '{{password}}';"},
	}

	path := fmt.Sprintf("database/static-roles/%s", roleName)

	resp, err := client.Logical().Write(path, data)
	if err != nil {
		logger.Error("can not create Vault role", zap.Error(err))
		return "", err
	}
	if resp == nil {
		err := fmt.Errorf("empty response from Vault")
		logger.Error("can not create Vault role", zap.Error(err))
		return "", err
	}
	logger.Info(fmt.Sprintf("Vault role %s has been created", roleName))
	return roleName, nil
}

func (c *Client) PrepareDbEngine(vaultExist bool, cluster *patroniv1.PatroniClusterSettings) error {
	if c.registration.DbEngine.Enabled {
		if !vaultExist {
			logger.Info("Activating Postgres Vault engine plugin")
			if _, err := c.vaultCreateDbEngine("vault-admin", cluster.PostgresServiceName, cluster.PgHost); err != nil {
				logger.Error("can not activate db Engine", zap.Error(err))
				return err
			}
			// Ensure metric collector role exist
			if err := createCollectorPgRole(cluster.PgHost); err != nil {
				logger.Info("Cannot create role for metric-collector", zap.Error(err))
			}
			// create Vault static roles
			c.createDbEngineRoles()

			if err := c.updatePatroniVaultRoles(cluster); err != nil {
				return err
			}
		}

		if err := c.UpdatePgClientPass(); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) updatePatroniVaultRoles(cluster *patroniv1.PatroniClusterSettings) error {
	statefulSets, err := c.helper.GetStatefulsetByNameRegExp(cluster.PatroniDeploymentName)
	if err != nil {
		return err
	}
	if c.IsEnvContainsVaultRole(statefulSets[0].Spec.Template.Spec.Containers[0].Env) {
		logger.Info("Patroni already have vault role env, skip deployments update")
		return nil
	}
	patrPods, err := c.helper.GetNamespacePodListBySelectors(cluster.PatroniCommonLabels)
	if err != nil {
		return err
	}
	if err = c.helper.UpdatePatroniReplicas(0, cluster.ClusterName); err != nil {
		return err
	}
	for _, patroniPod := range patrPods.Items {
		if err = util.WaitDeletePod(&patroniPod); err != nil {
			logger.Error("waiting for Patroni deployment upgrade failed", zap.Error(err))
			return err
		}
	}
	statefulSets, _ = c.helper.GetStatefulsetByNameRegExp(cluster.PatroniDeploymentName)
	for _, d := range statefulSets {
		// Scale up Patroni after update
		repl := int32(1)
		d.Spec.Replicas = &repl
		c.ProcessVaultSectionStatefulset(d, PatroniEntrypoint, roleSecrets)
		if err = c.helper.CreateOrUpdateStatefulset(d, true); err != nil {
			logger.Error(fmt.Sprintf("Cannot create or update deployment %s", d.Name), zap.Error(err))
			return err
		}
	}

	if err = util.WaitForPatroni(c.coreCr, cluster.PatroniMasterSelectors, cluster.PatroniReplicasSelector); err != nil {
		return err
	}
	return nil
}

func (c *Client) createDbEngineRoles() {
	for _, user := range roleSecrets {
		if _, err := c.CreatePostgresVaultRole(user); err != nil {
			logger.Error(fmt.Sprintf("can not create %s role", user), zap.Error(err))
		}
	}
}

func GetVaultRoleName(dbRole string) string {
	return fmt.Sprintf("%s_%s_%s_%s", util.GetServerHostname(), util.GetNameSpace(), "patroni-sa", dbRole) // No more variety there
}

func createCollectorPgRole(pgHost string) error {
	client := pgclient.GetPostgresClient(pgHost)
	// Password hardcode can be ignored, because it's gonna be rotated by vault
	if err := client.Execute("CREATE ROLE \"monitoring-user\" with login password 'p@ssWOrD1'"); err != nil {
		return err
	}
	return nil
}

func (c *Client) vaultGetToken(jwtToken string) string {
	options := map[string]interface{}{
		"jwt":  jwtToken,
		"role": util.GetServiceAccount(),
	}
	config := &api.Config{
		Address: c.registration.Url,
	}
	loginPath := getLoginPath()
	client, err := api.NewClient(config)
	if err != nil {
		fmt.Println(err)
	}
	clientToken, err := client.Logical().Write(loginPath, options)
	if err != nil {
		fmt.Println(err)
	}

	return string(clientToken.Auth.ClientToken)
}

func (c *Client) vaultSetToken(jwtToken string) *api.Client {
	config := &api.Config{
		Address: c.registration.Url,
	}
	client, err := api.NewClient(config)
	if err != nil {
		fmt.Println(err)
		return nil
	}
	clientToken := c.vaultGetToken(jwtToken)
	client.SetToken(clientToken)
	return client
}

func (c *Client) vaultRead(path string) (map[string]interface{}, bool) {
	token, err := util.ReadTokenFromFile()
	if err != nil {
		logger.Error("can not read the token from file")
		return nil, false
	}
	client := c.vaultSetToken(token)
	secret, err := client.Logical().Read(path)
	if err != nil {
		return nil, false
	}
	if secret == nil || secret.Data == nil {
		logger.Debug(fmt.Sprintf("No data on path %s", path))
		return nil, false
	}
	return secret.Data, true
}

func (c *Client) vaultWriteSecret(path string, secret map[string]interface{}) error {
	token, err := util.ReadTokenFromFile()
	if err != nil {
		logger.Error("can not read the token from file")
		return err
	}
	client := c.vaultSetToken(token)
	_, err = client.Logical().Write(path, secret)
	if err != nil {
		return err
	}
	return nil
}

func (c *Client) vaultCreateDbEngine(username string, postgresServiceName string, pgHost string) (bool, error) {
	dbEngName := c.registration.DbEngine.Name
	path := "/database/config/" + dbEngName
	token, err := util.ReadTokenFromFile()
	if err != nil {
		logger.Error("can not read the token from file")
		return false, err
	}
	if _, ok := c.vaultRead("database/config/" + dbEngName); ok {
		logger.Info(fmt.Sprintf("DbEngine %s already exists", dbEngName))
		return false, nil
	}

	pgClient := pgclient.GetPostgresClient(pgHost)
	conn, err := pgClient.GetConnection()
	if err != nil {
		return false, err
	}
	defer conn.Release()

	rows, err := conn.Query(context.Background(), fmt.Sprintf("SELECT 1 FROM pg_user WHERE usename = '%s'", pgclient.EscapeString(username)))
	if err != nil {
		logger.Error("error during obtaining vault user", zap.Error(err))
		return false, err
	}
	defer rows.Close()
	if rows.Next() {
		logger.Info("Vault user already exist")
		return false, nil
	}

	password := util.GenerateRandomPassword()
	err = pgClient.Execute(fmt.Sprintf("CREATE USER \"%s\" WITH PASSWORD '%s' SUPERUSER", username, password))
	if err != nil {
		logger.Error(fmt.Sprintf("create %s user error", username), zap.Error(err))
		return false, err
	}
	client := c.vaultSetToken(token)
	data := map[string]interface{}{
		"plugin_name":              "postgresql-database-plugin",
		"allowed_roles":            "*",
		"connection_url":           "postgresql://{{username}}:{{password}}@" + postgresServiceName + "." + util.GetNameSpace() + ":5432/postgres?sslmode=disable",
		"max_open_connections":     c.registration.DbEngine.MaxOpenConnections,
		"max_idle_connections":     c.registration.DbEngine.MaxIdleConnections,
		"max_connection_lifetime":  c.registration.DbEngine.MaxConnectionLifetime,
		"verify_connection":        false,
		"username":                 username,
		"password":                 password,
		"root_rotation_statements": []string{"ALTER USER \"{{username}}\" WITH PASSWORD '{{password}}';"},
	}
	_, err = client.Logical().Write(path, data)
	if err != nil {
		return false, err
	}

	if err = c.vaultRotateRootCreds(); err != nil {
		return false, err
	}

	return true, nil
}

func (c *Client) vaultRotateRootCreds() error {
	path := "/database/rotate-root/" + c.registration.DbEngine.Name
	token, err := util.ReadTokenFromFile()
	if err != nil {
		logger.Error("error reading token", zap.Error(err))
		return err
	}

	client := c.vaultSetToken(token)
	_, err = client.Logical().Write(path, nil)
	if err != nil {
		logger.Error("error during rotate root vault role", zap.Error(err))
		return err
	}
	return nil
}

func (c *Client) vaultRotateStaticCreds(username string) error {
	path := "/database/rotate-role/" + GetVaultRoleName(username)
	token, err := util.ReadTokenFromFile()
	if err != nil {
		logger.Error("cannot read token from file", zap.Error(err))
		return err
	}

	client := c.vaultSetToken(token)
	_, err = client.Logical().Write(path, nil)
	if err != nil {
		logger.Error("cannot rotate static creds", zap.Error(err))
		return err
	}
	return nil
}

func (c *Client) vaultRotatePgRoles() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, role := range roleSecrets {
		if !c.isMetricCollectorInstalled && role == "monitoring-user" {
			logger.Info("metric collector is not installed, skipping role rotate for monitoring user")
			continue
		}
		if err := c.vaultRotateStaticCreds(role); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) IsVaultRolesExist() bool {
	if !c.registration.Enabled && !c.registration.DbEngine.Enabled {
		return false
	}
	isRolesExist := true
	for _, role := range roleSecrets {
		if _, success := c.getVaultRoleData(role); !success {
			isRolesExist = false
		}
	}
	return isRolesExist
}

func (c *Client) getVaultRoleData(dbRole string) (map[string]interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	vaultRoleName := GetVaultRoleName(dbRole)
	resp, success := c.vaultRead(StaticCredsPath + vaultRoleName)
	if !success {
		logger.Info(fmt.Sprintf("Can't get data for vault role %s. Does role exist?", vaultRoleName))
		return nil, false
	}
	return resp, true
}

func getLoginPath() string {
	return "/auth/" + util.GetServerHostname() + "_" + util.GetNameSpace() + "/login"
}

func (c *Client) UpdatePgClientPass() error {
	resp, success := c.getVaultRoleData("postgres")
	if !success {
		return fmt.Errorf("cannot read vault role creds")
	}

	if password, ok := resp["password"]; ok {
		logger.Info("Update password for pg in operator")
		pgclient.UpdatePostgresClientPassword(password.(string))
	} else {
		logger.Error("Password not found in client creds")
		return fmt.Errorf("cannot rotate pg client creds")
	}
	return nil
}
