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
	"strings"

	"github.com/Netcracker/pgskipper-operator-core/pkg/util"
	utilOp "github.com/Netcracker/pgskipper-operator/pkg/util"
	"go.uber.org/zap"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	VaultPrefix     = "vault:"
	StaticCredsPath = "database/static-creds/"
)

var (
	DeletionLabels     = map[string]string{"set": "deletion"}
	PatroniEntrypoint  = []string{"/start.sh"}
	BackuperEntrypoint = []string{"/opt/backup/start_backup_daemon.sh"}
	MetricEntrypoint   = []string{"-c", "/monitor/metrics"}
	roleSecrets        = []string{"replicator", "postgres", "monitoring-user"}
)

func (c *Client) ProcessRoleSecret(secret *corev1.Secret) error {
	logger.Info(fmt.Sprintf("{%v}", c.registration))
	if c.registration.Enabled && !c.registration.DbEngine.Enabled {
		if err := c.MoveSecretToVault(secret); err == nil {
		} else {
			logger.Error(fmt.Sprintf("Cannot put secret %s to Vault", secret.Name), zap.Error(err))
			return err
		}
	} else if c.registration.DbEngine.Enabled {
		if err := c.LabelSecretToDeletion(secret); err == nil {
		} else {
			logger.Error(fmt.Sprintf("Cannot label secret %s to deletion", secret.Name), zap.Error(err))
			return err
		}
	}
	return nil
}

func (c *Client) MoveSecretToVault(secret *corev1.Secret) error {
	foundSecret := &corev1.Secret{}
	sec := make(map[string]interface{})
	err := c.k8sClient.Get(context.TODO(), types.NamespacedName{
		Name: secret.Name, Namespace: secret.Namespace,
	}, foundSecret)
	if err != nil && errors.IsNotFound(err) {
		logger.Info(fmt.Sprintf("Creating %s secret", secret.Name))
		return err
	}
	var vaultPath = c.registration.Path + "/" + secret.Name
	logger.Info(fmt.Sprintf("Putting %s secret to Vault", secret.Name))
	postgresSecret := foundSecret.Data
	for k, v := range postgresSecret {
		sec[k] = string(v)
	}
	if err = c.vaultWriteSecret(vaultPath, sec); err != nil {
		logger.Error("Vault write failed", zap.Error(err))
		return err
	}
	if err = c.LabelSecretToDeletion(secret); err != nil {
		logger.Error("Labeling fault", zap.Error(err))
		return err
	}
	return nil
}

func (c *Client) DeleteSecret(labelSelectors map[string]string) error {
	logger.Info("Deleting labeled secrets from k8s")
	secretList := &corev1.SecretList{}
	listOpts := []client.ListOption{
		client.InNamespace(util.GetNameSpace()),
		client.MatchingLabels(labelSelectors),
	}
	if err := c.k8sClient.List(context.Background(), secretList, listOpts...); err == nil {
		for secretIdx := 0; secretIdx < len(secretList.Items); secretIdx++ {
			vaultedSecret := secretList.Items[secretIdx]
			logger.Info(fmt.Sprintf("Delete secret %v", vaultedSecret.Name))
			err = c.k8sClient.Delete(context.TODO(), &vaultedSecret)
			if err != nil {
				logger.Error(fmt.Sprintf("Error delete secret %v", vaultedSecret.Name), zap.Error(err))
				return err
			}
		}
	}
	return nil
}

func (c *Client) LabelSecretToDeletion(secret *corev1.Secret) error {
	foundSecret := &corev1.Secret{}
	err := c.k8sClient.Get(context.TODO(), types.NamespacedName{
		Name: secret.Name, Namespace: secret.Namespace,
	}, foundSecret)
	if err != nil {
		logger.Info(fmt.Sprintf("Cant find %s secret", secret.Name))
		return err
	}
	lables := foundSecret.GetLabels()
	lables["set"] = "deletion"
	foundSecret.SetLabels(lables)
	_ = c.k8sClient.Update(context.Background(), foundSecret)
	return nil
}

func (c *Client) getEnvTemplateForVault(envName string, secretName string, secretKey string, vaultPath string) corev1.EnvVar {
	if secretKey == "password" {
		envValue := corev1.EnvVar{
			Name:  envName,
			Value: VaultPrefix + vaultPath + "/" + secretName + "#password",
		}
		return envValue
	} else {
		envValue := corev1.EnvVar{
			Name:  envName,
			Value: VaultPrefix + vaultPath + "/" + secretName + "#username",
		}
		return envValue
	}
}

func (c *Client) getEnvTemplateForVaultRole(envName string, secretName string, secretKey string) corev1.EnvVar {
	if secretKey == "password" {
		envValue := corev1.EnvVar{
			Name:  envName,
			Value: VaultPrefix + StaticCredsPath + GetVaultRoleName(secretName) + "#password",
		}
		return envValue
	} else {
		envValue := corev1.EnvVar{
			Name:  envName,
			Value: VaultPrefix + StaticCredsPath + GetVaultRoleName(secretName) + "#username",
		}
		return envValue
	}
}

func (c *Client) getVaultRegistrationEnv() []corev1.EnvVar {
	envValue := []corev1.EnvVar{
		{
			Name:  "VAULT_SKIP_VERIFY",
			Value: "True",
		},
		{
			Name:  "VAULT_ADDR",
			Value: c.registration.Url,
		},
		{
			Name:  "VAULT_PATH",
			Value: utilOp.GetServerHostname() + "_" + util.GetNameSpace(),
		},
		{
			Name:  "VAULT_ROLE",
			Value: utilOp.GetServiceAccount(),
		},
		{
			Name:  "VAULT_IGNORE_MISSING_SECRETS",
			Value: "False",
		},
		{
			Name:  "VAULT_ENV_PASSTHROUGH",
			Value: "VAULT_ADDR,VAULT_ROLE,VAULT_SKIP_VERIFY,VAULT_PATH,VAULT_ENABLED",
		},
	}
	return envValue
}

func (c *Client) getInitContainerTemplateForVault() []corev1.Container {
	initContainer := []corev1.Container{
		{
			Name:            "copy-vault-env",
			Image:           c.registration.DockerImage,
			SecurityContext: utilOp.GetDefaultSecurityContext(),
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: "/vault",
					Name:      "vault-env",
				},
			},
			Command: []string{
				"sh",
				"-c",
				"cp /usr/local/bin/vault-env /vault/",
			},
			Resources: corev1.ResourceRequirements{
				Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
				Limits: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceCPU:    resource.MustParse("50m"),
					corev1.ResourceMemory: resource.MustParse("50Mi"),
				},
			},
		},
	}
	return initContainer
}

func getVaultVolumeMount() corev1.VolumeMount {
	volumeMount := corev1.VolumeMount{
		MountPath: "/vault",
		Name:      "vault-env",
	}
	return volumeMount
}

func getVaultVolume() corev1.Volume {
	volume := corev1.Volume{
		Name: "vault-env",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: "Memory",
			},
		},
	}
	return volume
}

func getVaultCommand() []string {
	command := []string{
		"/vault/vault-env",
	}
	return command
}

func getVaultArgs(arguments []string) []string {
	args := []string{
		"bash",
	}
	args = append(args, arguments...)
	return args
}

func (c *Client) replaceEnvironments(envs []corev1.EnvVar, secretsForReplace []string) {
	// change postgres secrets to vault link
	for _, secret := range secretsForReplace {
		for k, v := range envs {
			if v.ValueFrom != nil && v.ValueFrom.SecretKeyRef != nil {
				if contain := strings.Contains(v.ValueFrom.SecretKeyRef.Name, secret); contain {
					if c.registration.DbEngine.Enabled && isRoleSecret(secret) {
						envs[k] = c.getEnvTemplateForVaultRole(v.Name, secret, v.ValueFrom.SecretKeyRef.Key)
					} else {
						envs[k] = c.getEnvTemplateForVault(v.Name, v.ValueFrom.SecretKeyRef.Name, v.ValueFrom.SecretKeyRef.Key, c.registration.Path)
					}

				}
			}
		}
	}
}

func (c *Client) ProcessVaultSectionStatefulset(stSet *appsv1.StatefulSet, entrypoint, secrets []string) {
	// Vault Section
	if c.registration.Enabled || c.registration.DbEngine.Enabled {
		// change postgres secrets to vault link
		c.replaceEnvironments(stSet.Spec.Template.Spec.Containers[0].Env, secrets)
		// add few Vault envs
		stSet.Spec.Template.Spec.Containers[0].Env = append(stSet.Spec.Template.Spec.Containers[0].Env, c.getVaultRegistrationEnv()...)
		// add volume mount to main container
		stSet.Spec.Template.Spec.Containers[0].VolumeMounts = append(stSet.Spec.Template.Spec.Containers[0].VolumeMounts, getVaultVolumeMount())
		// add vault emptydir volume to deployment
		stSet.Spec.Template.Spec.Volumes = append(stSet.Spec.Template.Spec.Volumes, getVaultVolume())
		// add vault command for getting credentials
		stSet.Spec.Template.Spec.Containers[0].Command = getVaultCommand()
		//Adding default security context
		stSet.Spec.Template.Spec.Containers[0].SecurityContext = utilOp.GetDefaultSecurityContext()
		// add args for start main container after
		stSet.Spec.Template.Spec.Containers[0].Args = getVaultArgs(entrypoint)

		for i := 0; i < len(stSet.Spec.Template.Spec.InitContainers); i++ {
			c.replaceEnvironments(stSet.Spec.Template.Spec.InitContainers[i].Env, secrets)
			stSet.Spec.Template.Spec.InitContainers[i].Env = append(stSet.Spec.Template.Spec.InitContainers[i].Env, c.getVaultRegistrationEnv()...)
			stSet.Spec.Template.Spec.InitContainers[i].VolumeMounts = append(stSet.Spec.Template.Spec.InitContainers[i].VolumeMounts, getVaultVolumeMount())
			stSet.Spec.Template.Spec.InitContainers[i].Command = getVaultCommand()
			stSet.Spec.Template.Spec.InitContainers[i].Args = append(getVaultArgs(entrypoint), stSet.Spec.Template.Spec.InitContainers[i].Args...)
		}

		stSet.Spec.Template.Spec.InitContainers = append(c.getInitContainerTemplateForVault(), stSet.Spec.Template.Spec.InitContainers...)
	}
}

func (c *Client) ProcessVaultSection(deployment *appsv1.Deployment, entrypoint, secrets []string) {
	// Vault Section
	if c.registration.Enabled || c.registration.DbEngine.Enabled {
		// change postgres secrets to vault link
		c.replaceEnvironments(deployment.Spec.Template.Spec.Containers[0].Env, secrets)
		// add few Vault envs
		deployment.Spec.Template.Spec.Containers[0].Env = append(deployment.Spec.Template.Spec.Containers[0].Env, c.getVaultRegistrationEnv()...)
		// add volume mount to main container
		deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[0].VolumeMounts, getVaultVolumeMount())
		// add vault emptydir volume to deployment
		deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, getVaultVolume())
		// add vault command for getting credentials
		deployment.Spec.Template.Spec.Containers[0].Command = getVaultCommand()
		//Adding default security context
		deployment.Spec.Template.Spec.Containers[0].SecurityContext = utilOp.GetDefaultSecurityContext()
		// add args for start main container after
		deployment.Spec.Template.Spec.Containers[0].Args = getVaultArgs(entrypoint)

		for i := 0; i < len(deployment.Spec.Template.Spec.InitContainers); i++ {
			c.replaceEnvironments(deployment.Spec.Template.Spec.InitContainers[i].Env, secrets)
			deployment.Spec.Template.Spec.InitContainers[i].Env = append(deployment.Spec.Template.Spec.InitContainers[i].Env, c.getVaultRegistrationEnv()...)
			deployment.Spec.Template.Spec.InitContainers[i].VolumeMounts = append(deployment.Spec.Template.Spec.InitContainers[i].VolumeMounts, getVaultVolumeMount())
			deployment.Spec.Template.Spec.InitContainers[i].Command = getVaultCommand()
			deployment.Spec.Template.Spec.InitContainers[i].Args = append(getVaultArgs(entrypoint), deployment.Spec.Template.Spec.InitContainers[i].Args...)
		}

		deployment.Spec.Template.Spec.InitContainers = append(c.getInitContainerTemplateForVault(), deployment.Spec.Template.Spec.InitContainers...)
	}
}

func (c *Client) ProcessPodVaultSection(pod *corev1.Pod, secrets []string) {
	if c.registration.Enabled {
		// change postgres secrets to vault link
		c.replaceEnvironments(pod.Spec.Containers[0].Env, secrets)
		// add few Vault envs
		pod.Spec.Containers[0].Env = append(pod.Spec.Containers[0].Env, c.getVaultRegistrationEnv()...)
		// add init container to deployment
		pod.Spec.InitContainers = c.getInitContainerTemplateForVault()
		// add volume mount to main container
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, getVaultVolumeMount())
		// add vault emptydir volume to deployment
		pod.Spec.Volumes = append(pod.Spec.Volumes, getVaultVolume())
		// add vault command for getting credentials
		pod.Spec.Containers[0].Command = getVaultCommand()
	}
}

func (c *Client) IsEnvContainsVaultRole(envVars []corev1.EnvVar) bool {
	for _, env := range envVars {
		if strings.Contains(env.Value, VaultPrefix+StaticCredsPath) {
			return true
		}
	}
	return false
}

func isRoleSecret(value string) bool {
	for _, v := range roleSecrets {
		if strings.Contains(value, v) {
			return true
		}
	}
	return false
}
