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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sLabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

var Log = GetLogger()

var (
	tmpDir       = "/tmp"
	debugEnabled = GetEnv("DEBUG_ENABLED", "false")
)

const certificatesFolder = "/certs"

func GetLogger() *zap.Logger {
	cfg := zap.NewProductionConfig()
	cfg.Encoding = "console"
	cfg.EncoderConfig.TimeKey = "timestamp"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.EncoderConfig.CallerKey = ""
	cfg.EncoderConfig.EncodeLevel = CustomLevelEncoder
	if debugEnabled == "true" {
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	}
	cfg.OutputPaths = []string{
		"/proc/1/fd/1",
	}
	logger, err := cfg.Build()
	if err != nil {
		log.Printf("Cannot create logger: %s", err.Error())
	}

	defer func() {
		_ = logger.Sync()
	}()
	return logger
}

func GetProtocol() (string, string) {
	if GetEnv("TLS", "false") == "true" {
		return "https://", "8530"
	}
	return "http://", "8529"

}

func GetToken() string {
	tokenByte, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		log.Fatal(err)
	}
	token := string(tokenByte[:])
	return token
}

func GetPodInfo(client *http.Client, token, remoteHost, remotePort, project, selector, selectorValue string) map[string]interface{} {

	startTime := time.Now()
	hostUrl := GetHostUrl(remoteHost, remotePort)
	podsDataUrl := fmt.Sprintf("%s/api/v1/namespaces/%s/pods?labelSelector=%s=%s", hostUrl, project, selector, selectorValue)
	Log.Debug(fmt.Sprintf("ProcessHttpRequest: client: %v, podsDataUrl: %s: token: %s", *client, podsDataUrl, token))
	status, response, err := ProcessHttpRequest(client, podsDataUrl, token)
	Log.Debug(fmt.Sprintf("Load pods info time for %s=%s: %v", selector, selectorValue, time.Since(startTime)))
	statusCode, _ := strconv.Atoi(status)
	if err != nil {
		Log.Error(fmt.Sprintf("Load pods info time for %s=%s: Error: %s", selector, selectorValue, err))
	}
	if statusCode >= 400 {
		panic(fmt.Sprintf("Cannot collect pods data with code %v", statusCode))
	}
	var res map[string]interface{}
	err = json.Unmarshal(response, &res)
	if err != nil {
		Log.Error(fmt.Sprintf("Load pods info Unmarshal Error: %s", err))
		return res
	}
	Log.Debug(fmt.Sprintf("Collected info about %s=%s pods: %s", selector, selectorValue, res))
	return res

}

func GetLeaderPod(client *http.Client, token, clusterName string) map[string]interface{} {

	url := fmt.Sprintf("http://pg-%s-api:8008/cluster", clusterName)

	status, response, err := ProcessHttpRequest(client, url, token)
	if err != nil {
		Log.Warn(fmt.Sprintf("Error, while http request to find leader patroni pod: %s", err))
	}
	statusCode, _ := strconv.Atoi(status)
	if statusCode >= 400 {
		panic(fmt.Sprintf("Cannot collect pods data with code %v", statusCode))
	}
	var res map[string]interface{}
	err = json.Unmarshal(response, &res)
	if err != nil {
		//TODO: log here
		return res
	}
	members := SafeGet(res, append(make([]interface{}, 0), "members"), make(map[string]interface{}, 0))
	for _, member := range members.([]interface{}) {
		role := SafeGet(member, append(make([]interface{}, 0), "role"), "").(string)
		if role == "leader" || role == "standby_leader" {
			return member.(map[string]interface{})
		}
	}
	Log.Warn("Can not find leader of patroni cluster")
	return res
}

func ProcessHttpRequest(client *http.Client, url string, token string) (string, []byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", nil, err
	}
	var bearer = "bearer " + token
	req.Header.Set("Authorization", bearer)
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}
	return resp.Status, body, nil
}

func GetHostUrl(remoteHost, remotePort string) string {
	//if isIpv4(remoteHost) {
	return fmt.Sprintf("https://%s:%s", remoteHost, remotePort)
	//} else {
	//	return fmt.Sprintf("https://[%s]:%s", remoteHost, remotePort)
	//}
}

func GetServiceList(client *kubernetes.Clientset, namespace string, selector string) (*corev1.ServiceList, error) {
	opts := metav1.ListOptions{
		LabelSelector: selector,
	}
	serviceList, err := client.CoreV1().Services(namespace).List(context.TODO(), opts)
	if len(serviceList.Items) == 0 {
		Log.Info(fmt.Sprintf("Service with label %s not found", selector))
		return nil, err
	} else if err != nil {
		return nil, err
	}
	Log.Info(fmt.Sprintf("Found Service with label %s", selector))
	return serviceList, nil
}

func GetPodList(client *kubernetes.Clientset, namespace string, selector map[string]string) (*corev1.PodList, error) {
	opts := metav1.ListOptions{
		LabelSelector: k8sLabels.SelectorFromSet(selector).String(),
	}
	podList, err := client.CoreV1().Pods(namespace).List(context.TODO(), opts)
	if len(podList.Items) == 0 {
		Log.Debug(fmt.Sprintf("Pod with label %s not found", selector))
		return nil, err
	} else if err != nil {
		return nil, err
	}
	Log.Debug(fmt.Sprintf("Found Pod with label %s", selector))
	return podList, nil
}

func GetKafkaMetadata(ctx context.Context, client *kgo.Client) (*kmsg.MetadataResponse, error) {
	req := kmsg.NewMetadataRequest()
	req.Topics = nil

	var err error
	var metadata *kmsg.MetadataResponse
	if metadata, err = req.RequestWith(ctx, client); err == nil {
		return metadata, nil
	}
	return nil, err

}

func GetHttpClient() *http.Client {
	var client *http.Client
	var tlsClientConfig *tls.Config
	if GetEnv("TLS", "false") == "true" {
		certsDir, err := os.ReadDir(certificatesFolder)
		infos := make([]fs.FileInfo, 0, len(certsDir))
		for _, entry := range certsDir {
			info, err := entry.Info()
			if err != nil {
				log.Printf("Cannot certificate from file '%s'. Maybe deleted or moved.", entry.Name())
			}
			infos = append(infos, info)
		}
		if err != nil || len(infos) == 0 {
			log.Printf("Cannot load TLS certificates from path '%s'. InsecureSkipVerify is used.", certificatesFolder)

		} else {
			certs := x509.NewCertPool()
			for _, cert := range infos {
				if isNotDir(cert) {
					pemData, err := os.ReadFile(fmt.Sprintf("%s/%s", certificatesFolder, cert.Name()))
					if err != nil {
						log.Panicf(fmt.Sprintf("Failed to read certificate '%s'", cert.Name()), zap.Error(err))
					}
					certs.AppendCertsFromPEM(pemData)
					log.Printf("Trusted certificate '%s' was added to client", cert.Name())
				}
			}
			tlsClientConfig = &tls.Config{RootCAs: certs}
			transport := &http.Transport{TLSClientConfig: tlsClientConfig}
			client = &http.Client{Transport: transport, Timeout: 10 * time.Second}
			return client
		}
	}
	client = &http.Client{Timeout: 10 * time.Second}
	return client
}

func isNotDir(info os.FileInfo) bool {
	return !info.IsDir() && !strings.HasPrefix(info.Name(), "..")
}

func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func GetEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if ivalue, err := strconv.Atoi(value); err == nil {
			return ivalue
		}
	}
	return fallback
}

func GetEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		bvalue, err := strconv.ParseBool(value)
		if err != nil {
			Log.Error(fmt.Sprintf("Can't parse %s boolean variable", key), zap.Error(err))
		}
		return bvalue
	}
	return fallback
}

func DetermineRole(pod corev1.Pod) string {
	podLabels := pod.Labels
	pgType, ok := podLabels["pgtype"]
	if ok {
		return pgType
	}
	return "replica"

}

func SafeGet(data interface{}, path []interface{}, defaultValue interface{}) interface{} {
	var result interface{}
	if data == nil {
		result = defaultValue
	}
	for _, pathElement := range path {
		switch v := data.(type) {
		case map[string]interface{}:
			if path, ok := pathElement.(string); ok {
				if cur, ok := v[path]; ok {
					result = cur
				} else {
					result = defaultValue
				}
			} else {
				Log.Debug(fmt.Sprintf("SafeGet: type is map[string]interface{}. Not Valid Path element Data: %s, PathElement: %s", v, pathElement))
				result = defaultValue
			}
		case []interface{}:
			if path, ok := pathElement.(int); ok {
				if len(v) > path {
					result = v[path]
				} else {
					result = defaultValue
				}
			} else {
				result = defaultValue
			}
		case string:
			var res map[string]interface{}
			err := json.Unmarshal([]byte(v), &res)
			if err != nil {
				Log.Error(fmt.Sprintf("SafeGet: Can not convert current string element to json: %s", v))
				return v
			}
			return SafeGet(res, append(make([]interface{}, 0), pathElement), "")
		default:
			result = data
		}
	}
	return result
}

func Contains(slice []string, value string) bool {
	for _, v := range slice {
		if v == value {
			return true
		}
	}
	return false
}

func GetLatestMasterAppearance() (string, string) {

	lma := GetPodValue("master", "last_master_appearance", "")
	lmxl := GetPodValue("master", "last_master_xlog_location", "0")
	return lma, lmxl
}

func StoreLastMasterAppearance(xlogLocation float64) {

	StorePodValue("master", "last_master_appearance", strconv.Itoa(time.Now().Second()))

	exValue := GetPodValue("master", "last_master_xlog_location", "0")
	val, err := strconv.ParseFloat(exValue, 64)
	if err != nil {
		Log.Error("Error while storing master appearance", zap.Error(err))
	}
	res := math.Max(val, xlogLocation)

	StorePodValue("master", "last_master_xlog_location", fmt.Sprintf("%f", res))
}

func StorePodValue(pod, key, value string) {

	fName := fmt.Sprintf("%s/%s.%s.tmp", tmpDir, pod, key)

	Log.Debug(fmt.Sprintf("Store pod key: %s, value: %s to file: %s", key, value, fName))

	mydata := []byte(value)
	if err := os.WriteFile(fName, mydata, 0777); err != nil {
		Log.Error(fmt.Sprintf("Error while saving pod value: %s", value), zap.Error(err))
	}

}
func GetPodValue(pod, key, defaultValue string) string {

	fName := fmt.Sprintf("%s/%s.%s.tmp", tmpDir, pod, key)

	Log.Debug(fmt.Sprintf("Get pod key: %s from file: %s", key, fName))

	data, err := os.ReadFile(fName)
	if err != nil {
		Log.Warn(fmt.Sprintf("Error while reading pod value of %s", key))
		Log.Debug(fmt.Sprintf("Error while reading pod value of %s", key), zap.Error(err))
		return defaultValue
	}
	return string(data)
}

func GetPodStatus(pod corev1.Pod) string {
	state := ""
	status := SafeGet(pod.Annotations["status"], append(make([]interface{}, 0), "state"), "")
	if status == "" {
		status = pod.Status.ContainerStatuses[0].State
		Log.Debug(fmt.Sprintf("pod status: %s", status))
		for _, st := range status.([]string) {
			state = st
		}
	} else {
		Log.Debug(fmt.Sprintf("pod status: %s", status))
		state = fmt.Sprintf("%s", status)
	}
	return state
}

func CustomLevelEncoder(level zapcore.Level, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString("[" + level.CapitalString() + "]")
}

func CreateClient() (crclient.Client, error) {
	clientConfig, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	client, err := crclient.New(clientConfig, crclient.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return client, nil
}

func GetFloatValue(value any, defaultValue float64) (float64, error) {
	var result float64
	switch v := value.(type) {
	default:
		return defaultValue, fmt.Errorf("unexpected type %T. Can not parse value", v)
	case int:
		result = float64(v)
	case int32:
		result = float64(v)
	case int64:
		result = float64(v)
	case float64:
		result = v
	case string:
		if v == "" || strings.EqualFold(v, "None") {
			return defaultValue, errors.New("unexpected empty value")
		}
		if strings.EqualFold(v, "true") {
			result = float64(1)
		} else if strings.EqualFold(v, "false") {
			result = float64(0)
		} else {
			if val, err := strconv.ParseFloat(v, 64); err != nil {
				return defaultValue, fmt.Errorf("can't parse value '%v'", v)
			} else {
				result = val
			}
		}
	case bool:
		if v {
			result = float64(1)
		} else {
			result = float64(0)
		}
	case pgtype.Numeric:
		if v.Valid {
			result, _ = new(big.Float).SetInt(v.Int).Float64()
		} else {
			result = float64(0)
		}
	}
	return result, nil
}

func ExecCmdOnPod(client *kubernetes.Clientset, podName string, namespace string, command string) (string, string, error) {

	Log.Debug(fmt.Sprintf("Executing shell command: %s  on pod %s", command, podName))
	config, err := rest.InClusterConfig()
	if err != nil {
		panic(err.Error())
	}

	buf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	request := client.CoreV1().RESTClient().
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Command: []string{"/bin/sh", "-c", command},
			Stdin:   false,
			Stdout:  true,
			Stderr:  true,
			TTY:     true,
		}, clientgoscheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", request.URL())
	if err != nil {
		Log.Error(fmt.Sprintf("Executing shell command: Error: \n%v\nerrBuf: %v", err, errBuf))
		return "", "", fmt.Errorf("%w Failed executing command %s on %v/%v", err, command, namespace, podName)
	}
	err = exec.StreamWithContext(context.TODO(), remotecommand.StreamOptions{
		Stdout: buf,
		Stderr: errBuf,
	})
	if err != nil {
		Log.Error(fmt.Sprintf("Executing shell command: Error: \n%v\nerrBuf: %v", err, errBuf))
		return "", "", fmt.Errorf("%w Failed executing command %s on %v/%v", err, command, namespace, podName)
	}
	return buf.String(), errBuf.String(), nil
}

func GetPodIdentity(pod corev1.Pod) string {
	podIdentity := GetEnvValueFromPod(pod, "POD_IDENTITY", "node")

	podId := pod.Name
	podIp := pod.Status.PodIP
	if podIdentity == "node" {
		if podIdentity[0:10] == "postgresql" {
			podIdentity = "node1"
		} else {
			num, _ := strconv.Atoi(podId[len(podIp)-1:])
			podIdentity = "node" + strconv.Itoa(num+1)
		}
	}
	return podIdentity
}

func GetEnvValueFromPod(pod corev1.Pod, envName, defaultValue string) string {

	envs := pod.Spec.Containers[0].Env
	for _, env := range envs {
		if env.Name == envName {
			return env.Value
		}
	}
	return defaultValue
}
