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

package util

import (
	"context"
	cRand "crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"reflect"
	r "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Netcracker/pgskipper-operator-core/pkg/util"
	qubershipv1 "github.com/Netcracker/pgskipper-operator/api/apps/v1"
	patroniv1 "github.com/Netcracker/pgskipper-operator/api/patroni/v1"
	"golang.org/x/crypto/ssh"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	TokenFilePath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	ClusterName   = "patroni"
)

var (
	uLog           = GetLogger()
	namespace      = GetNameSpace()
	k8sClient      crclient.Client
	reconcileMutex sync.Mutex
)

func GetNameSpace() string {
	return os.Getenv("WATCH_NAMESPACE")
}

func GetServerHostname() string {
	return os.Getenv("CLOUD_PUBLIC_HOST")
}

func GetServiceAccount() string {
	role := strings.ToLower(os.Getenv("OPERATOR_ROLE"))
	defValue := "postgres-sa"
	if role == "patroni" {
		defValue = "patroni-sa"
	}
	return GetEnv("SERVICE_ACCOUNT", defValue)
}

func GetEnv(key string, def string) string {
	v := os.Getenv(key)
	if len(v) == 0 {
		return def
	}
	return v
}

func GetEnvAsInt(key string, def int) int {
	v := os.Getenv(key)
	if len(v) == 0 {
		return def
	}
	if intv, e := strconv.Atoi(v); e == nil {
		return intv
	}
	return def
}

func GetLogger() *zap.Logger {
	atom := zap.NewAtomicLevel()
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	))
	defer func() {
		_ = logger.Sync()
	}()
	return logger
}

func Merge(ms ...map[string]string) map[string]string {
	res := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			res[k] = v
		}
	}
	return res
}

func ReadFromFile(filePath string) (string, error) {
	dat, err := os.ReadFile(filePath)
	if err != nil {
		uLog.Error(fmt.Sprintf("cannot read from file %s", filePath), zap.Error(err))
		return "", err
	}
	return string(dat), nil
}

func ReadTokenFromFile() (string, error) {
	return ReadFromFile(TokenFilePath)
}

func GetClient() (crclient.Client, error) {
	if k8sClient == nil {
		client, err := createClient()
		if err != nil {
			return nil, err
		}
		k8sClient = client
	}
	return k8sClient, nil
}

func createClient() (crclient.Client, error) {
	clientConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(qubershipv1.AddToScheme(scheme))
	utilruntime.Must(patroniv1.AddToScheme(scheme))

	client, err := crclient.New(clientConfig, crclient.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func GetSmAuthUserName() string {
	smNs := os.Getenv("SM_NAMESPACE")
	smSaName := os.Getenv("SM_AUTH_SA")
	return fmt.Sprintf("system:serviceaccount:%s:%s", smNs, smSaName)
}

func IsHttpAuthEnabled() bool {
	return strings.ToLower(util.GetEnv("SM_HTTP_AUTH", "false")) == "true"
}

func InternalTlsEnabled() string {
	return strings.ToLower(util.GetEnv("INTERNAL_TLS_ENABLED", "false"))
}

func GetKubeClient() *kubernetes.Clientset {
	k8sConfig, err := rest.InClusterConfig()
	if err != nil {
		panic(err)
	}
	k8sConfig.Timeout = 60 * time.Second
	client, err := kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		panic(err)
	}
	return client
}

func GetPatroniClusterName(patroniClusterName string) string {
	if patroniClusterName != "" {
		return patroniClusterName
	}
	return ClusterName
}

func GetPatroniClusterSettings(patroniClusterName string) *patroniv1.PatroniClusterSettings {
	clusterName := ClusterName
	if patroniClusterName != "" {
		clusterName = patroniClusterName
	}
	pgServiceName := fmt.Sprintf("pg-%s", clusterName)
	pgReplicasServiceName := fmt.Sprintf("pg-%s-ro", clusterName)
	patroniUrl := fmt.Sprintf("http://pg-%s-api:8008/", clusterName)
	patroniTemplate := fmt.Sprintf("%s-patroni.config.yaml", clusterName)
	postgreSQLUserConf := fmt.Sprintf("postgres-%s.properties", clusterName)
	patroniDeploymentName := fmt.Sprintf("pg-%s-node", clusterName)
	pgHost := fmt.Sprintf("pg-%s.%s.svc.cluster.local", clusterName, namespace)

	return &patroniv1.PatroniClusterSettings{
		ClusterName:                clusterName,
		PatroniLabels:              map[string]string{"app": clusterName, "pgcluster": clusterName},
		PatroniCommonLabels:        map[string]string{"app": clusterName},
		PostgresServiceName:        pgServiceName,
		PatroniMasterSelectors:     map[string]string{"pgtype": "master", "pgcluster": clusterName},
		PatroniReplicasSelector:    map[string]string{"pgtype": "replica", "pgcluster": clusterName},
		PatroniReplicasServiceName: pgReplicasServiceName,
		PatroniUrl:                 patroniUrl,
		PatroniCM:                  "patroni.config.yaml",
		PatroniPropertiesCM:        "postgres",
		PatroniTemplate:            patroniTemplate,
		ConfigMapKey:               "patroni-config-template.yaml",
		PostgreSQLUserConf:         postgreSQLUserConf,
		PostgreSQLPort:             5432,
		PatroniDeploymentName:      patroniDeploymentName,
		PgHost:                     pgHost,
	}
}

func GetConfigMapByName(configMapLocalName string, configMapName string, configMapKey string) *corev1.ConfigMap {
	namespace := util.GetNameSpace()
	filePath := fmt.Sprintf("/opt/operator/%s", configMapLocalName)
	bytes, e := os.ReadFile(filePath)
	if e != nil {
		uLog.Error("Failed to read from file", zap.Error(e))
	}
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			configMapKey: string(bytes),
		},
	}
}

func GenerateRandomPassword() string {
	chars := []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
		"abcdefghijklmnopqrstuvwxyz" +
		"0123456789")
	length := 8
	var b strings.Builder
	for i := 0; i < length; i++ {
		b.WriteRune(chars[rand.Intn(len(chars))])
	}
	return b.String()
}

func ExecuteWithRetries(f func() error) error {
	try := 0
	uLog.Info(fmt.Sprintf("will execute next func with retries: %s", r.FuncForPC(reflect.ValueOf(f).Pointer()).Name())) //TODO: check this log
	for {
		var err error
		try += 1
		if err = f(); err == nil {
			return nil
		} else {
			uLog.Error("there is an error during func call, ", zap.Error(err))
		}
		if try == 10 {
			uLog.Info("no more retries, giving up")
			return err
		}
		time.Sleep(10 * time.Second)
	}
}

func GetTlsSecretVolumeMount() corev1.VolumeMount {
	volumeMount := corev1.VolumeMount{
		MountPath: "/certs/",
		Name:      "tls-cert",
		ReadOnly:  true,
	}
	return volumeMount
}

func GetTlsSecretVolume(certificateSecretName string) corev1.Volume {
	mode := int32(416)
	volume := corev1.Volume{
		Name: "tls-cert",
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  certificateSecretName,
				DefaultMode: &mode,
			},
		},
	}
	return volume
}

func ReconcileMutexLock() {
	reconcileMutex.Lock()
}

func ReconcileMutexLockUnlock() {
	reconcileMutex.Unlock()
}

func GetShmVolumeMount() corev1.VolumeMount {
	volumeMount := corev1.VolumeMount{
		MountPath: "/dev/shm",
		Name:      "dshm",
	}
	return volumeMount
}

func GetShmVolume() corev1.Volume {
	volume := corev1.Volume{
		Name: "dshm",
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{
				Medium: "Memory",
			},
		},
	}
	return volume
}

// https://stackoverflow.com/q/64179218

type Retry struct {
	nums      int
	transport http.RoundTripper
}

func (r *Retry) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	uLog.Info(fmt.Sprintf("executing request: %s with retries", req.URL))
	for i := 0; i < r.nums; i++ {
		uLog.Info(fmt.Sprintf("attempt: %v  of  %v", i+1, r.nums))
		resp, err = r.transport.RoundTrip(req)
		if err != nil || resp.StatusCode >= 500 {
			uLog.Warn(fmt.Sprintf("received retryable error %v, with status code: %v", err, resp.StatusCode))
			time.Sleep(10 * time.Second)
			continue
		} else {
			uLog.Info("executed successfully, exiting")
			return
		}
	}
	uLog.Warn("no more retries, giving up ...")
	return
}

func GetRetryTransport() *Retry {
	return &Retry{transport: http.DefaultTransport, nums: 10}
}

func GetDefaultSecurityContext() *corev1.SecurityContext {
	falseValue := false
	trueValue := true
	if strings.ToLower(os.Getenv("GLOBAL_SECURITY_CONTEXT")) == "true" {
		return &corev1.SecurityContext{RunAsNonRoot: &trueValue,
			SeccompProfile: &corev1.SeccompProfile{
				Type: corev1.SeccompProfileTypeRuntimeDefault,
			},
			AllowPrivilegeEscalation: &falseValue,
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{"ALL"},
			},
		}
	}
	return nil
}

func FindCmInNamespaceByName(namespace string, name string) (*corev1.ConfigMap, error) {
	foundConfigMap := &corev1.ConfigMap{}
	err := k8sClient.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: namespace}, foundConfigMap)
	return foundConfigMap, err
}

func GetContainerNameForPatroniPod(podName string) string {
	re := regexp.MustCompile(`node(\d+)`)
	if texts := re.FindStringSubmatch(podName); len(texts) > 1 {
		return "pg-patroni-node" + texts[1]
	}
	return "pg-patroni-node1"
}

func SliceContains[T comparable](slice []T, value T) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

func GenerateSSHKeyPair(bits int) (privateKeyPEM string, publicKeyOpenSSH string, err error) {
	privateKey, err := rsa.GenerateKey(cRand.Reader, bits)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate private key: %v", err)
	}

	privDER := x509.MarshalPKCS1PrivateKey(privateKey)
	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	})

	pub, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate public key: %v", err)
	}
	pubKeyStr := string(ssh.MarshalAuthorizedKey(pub))

	return string(privPEM), pubKeyStr, nil
}
