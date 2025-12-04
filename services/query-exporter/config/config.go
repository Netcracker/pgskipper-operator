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
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Netcracker/qubership-query-exporter/utils"

	"github.com/Netcracker/qubership-query-exporter/crypto"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

var allowedMetricTypes = map[string]bool{
	"histogram": true,
	"gauge":     true,
	"counter":   true,
}

type Config struct {
	DbName         string          `yaml:"-"`
	Db             *DatabaseConfig `yaml:"-"`
	Databases      map[string]*DatabaseConfig
	Metrics        map[string]*MetricsConfig
	Queries        map[string]*QueryConfig
	SelfMonitoring SelfMonitoringConfig `yaml:"self-monitoring,omitempty"`
	Flags          map[string]string    `yaml:"flags,omitempty"`
	ErrorHandling  *ErrorHandlingConfig `yaml:"error-handling,omitempty"`
}

type DatabaseConfig struct {
	Type         string `yaml:"type,omitempty"`
	Dsn          string
	DecryptedDsn string `yaml:"-"`
	KeepCon      bool   `yaml:"keep-connected"`
	Labels       map[string]string
	InitSqls     []string `yaml:"init-sql"`
}

type MetricsConfig struct {
	Type           string
	Description    string
	Labels         []string          `yaml:",flow"`
	ConstLabels    map[string]string `yaml:"const-labels,omitempty"`
	Sum            string
	Summarize      *bool `yaml:"summarize,omitempty"`
	Count          string
	Buckets        map[string]float64
	Parameters     map[string]string
	ExpectedLabels []map[string][]string `yaml:"expected-labels,flow"`
}

type QueryConfig struct {
	Master           bool
	Databases        []string     `yaml:",flow"`
	LogicalDatabases []string     `yaml:"logicalDatabases,omitempty"`
	Classifiers      []Classifier `yaml:"classifiers,omitempty"`
	Interval         string
	Croniter         string
	Metrics          []string `yaml:",flow"`
	Timeout          string
	Parameters       []map[string]string
	Sql              string
}

type Classifier map[string]string

type SelfMonitoringConfig struct {
	QueryLatencyBuckets []float64 `yaml:"query-latency-buckets,flow"`
}

type ErrorHandlingConfig struct {
	DbErrorRules map[string]*DbErrorRule `yaml:"db-error-rules,flow"`
}

type DbErrorRule struct {
	Action   string
	Contains string
	Timeout  string
}

var (
	keyPath = flag.String("key-path", "", "Path to the key for dsn encryption")
)

const (
	enc_prefix = "{ENC}"
	keySize    = 32
)

func Read(path string) (*Config, error) {
	config := Config{}
	configFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("Error opening config file %v : %+v", path, err)
	} else {
		defer configFile.Close()
	}

	buf := bytes.Buffer{}

	length, err := io.Copy(&buf, configFile)
	if err != nil {
		return nil, fmt.Errorf("Error copying config file %v : %+v", path, err)
	}
	log.Debugf("Copied %v bytes successfully to the buffer from file %v", length, path)

	err = yaml.Unmarshal(buf.Bytes(), &config)
	if err != nil {
		return nil, fmt.Errorf("Error unmarshalling config file %v : %+v", path, err)
	}

	err = validateConfig(&config)
	if err != nil {
		return nil, fmt.Errorf("Error vaildating config file %v : %+v", path, err)
	}

	err = processCrypto(path, &config)
	if err != nil {
		return nil, fmt.Errorf("Crypto error for config %v : %+v", path, err)
	}

	config.initDefaultDB()
	return &config, nil
}

func validateConfig(config *Config) error {
	startupBlockingErrors := make([]string, 0)
	log.Infof("CONFIG VALIDATION STARTED")
	if len(config.Databases) == 0 {
		return fmt.Errorf("Databases are not defined")
	}
	for dbName, dbConfig := range config.Databases {
		if dbConfig.Dsn == "" {
			return fmt.Errorf("Database %v has empty Dsn", dbName)
		}
		for label, labelValue := range dbConfig.Labels {
			if labelValue == "" {
				log.Warnf("Label %v for database %v has empty value", label, dbName)
			}
		}
	}

	if len(config.Queries) == 0 {
		return fmt.Errorf("Queries are not defined")
	}

	if len(config.Metrics) == 0 {
		log.Warnf("Metrics are not defined in yaml")
	} else {
		for mName, mCfg := range config.Metrics {
			if mCfg == nil {
				log.Errorf("Metric %v has empty configuration", mName)
				continue
			}
			if mCfg.Description == "" {
				log.Warnf("Metric %v has empty description", mName)
			}
			if mCfg.Type == "" {
				log.Errorf("Metric %v has empty type", mName)
			} else if !allowedMetricTypes[mCfg.Type] {
				log.Errorf("Metric %v has not supported type %v", mName, mCfg.Type)
			}
			checkHistogramFields(startupBlockingErrors, mName, mCfg)
			if !checkExpectedLabelsFields(mName, mCfg) {
				log.Warnf("Expected labels for metric %v were reset to nil", mName)
				mCfg.ExpectedLabels = nil
			}
		}
	}

	parser := utils.GetCronParser()

	for qName, qCfg := range config.Queries {
		if len(qCfg.Databases) == 0 {
			log.Errorf("Databases for query %v are not defined in yaml", qName)
		} else {
			for _, dbName := range qCfg.Databases {
				if config.Databases[dbName] == nil {
					log.Errorf("Database %v for query %v is not defined in yaml", dbName, qName)
				}
			}
		}
		if qCfg.Timeout != "" {
			_, err := time.ParseDuration(qCfg.Timeout)
			if err != nil {
				log.Errorf("Failed to parse timeout %v for query %v : %+v", qCfg.Timeout, qName, err)
			}
		}
		if len(qCfg.Metrics) == 0 {
			log.Warnf("For query %v metrics are not defined; dynamic logic will be applied, columns will be interpreted as [MetricName][MetricValue][LabelValue1]...[LabelValueN]", qName)
		} else {
			for _, metricName := range qCfg.Metrics {
				if config.Metrics[metricName] == nil {
					errorMessage := fmt.Sprintf("Metric %v for query %v is not defined in yaml", metricName, qName)
					startupBlockingErrors = append(startupBlockingErrors, errorMessage)
					log.Error(errorMessage)
				}
			}
		}
		if qCfg.Croniter == "" && qCfg.Interval == "" {
			log.Warnf("Query %v has both croniter and interval fields empty. Query will be executed on scrape event", qName)
		} else {
			if qCfg.Croniter != "" {
				_, parseErr := parser.Parse(qCfg.Croniter)
				if parseErr != nil {
					log.Errorf("Query %v has invalid croniter expression: %v ; %+v", qName, qCfg.Croniter, parseErr)
				}
			}
			if qCfg.Interval != "" {
				_, parseErr := parser.Parse("@every " + qCfg.Interval)
				if parseErr != nil {
					log.Errorf("Query %v has invalid interval expression: %v ; %+v", qName, qCfg.Interval, parseErr)
				}
			}
			if qCfg.Croniter != "" && qCfg.Interval != "" {
				log.Warnf("Query %v has both croniter and interval expressions defined. Croniter expression will be used if it is valid", qName)
			}
		}
	}

	log.Infof("CONFIG VALIDATION FINISHED")

	if len(startupBlockingErrors) != 0 {
		return fmt.Errorf("Query exporter can not start, reasons: %+v", startupBlockingErrors)
	}
	return nil
}

func checkHistogramFields(startupBlockingErrors []string, mName string, mCfg *MetricsConfig) {
	if mCfg.Type == "histogram" {
		if len(mCfg.Buckets) == 0 {
			errorMessage := fmt.Sprintf("Buckets are not defined for histogram metric %v", mName)
			startupBlockingErrors = append(startupBlockingErrors, errorMessage)
		}
		if mCfg.Sum == "" {
			errorMessage := fmt.Sprintf("Sum field is not defined for histogram metric %v", mName)
			startupBlockingErrors = append(startupBlockingErrors, errorMessage)
		}
		if mCfg.Count == "" {
			errorMessage := fmt.Sprintf("Count field is not defined for histogram metric %v", mName)
			startupBlockingErrors = append(startupBlockingErrors, errorMessage)
		}
	} else {
		if len(mCfg.Buckets) != 0 {
			log.Errorf("Buckets are defined for non-histogram metric %v, the field will be ignored", mName)
		}
		if mCfg.Sum != "" {
			log.Errorf("Sum is defined for non-histogram metric %v, the field will be ignored", mName)
		}
		if mCfg.Count != "" {
			log.Errorf("Count is defined for non-histogram metric %v, the field will be ignored", mName)
		}
	}
}

func checkExpectedLabelsFields(mName string, mCfg *MetricsConfig) bool {
	labelsCount := len(mCfg.Labels)
	for itemNum, expectedLabelsItem := range mCfg.ExpectedLabels {
		if len(expectedLabelsItem) != labelsCount {
			log.Errorf("Invalid expected labels configuration for metric %v, itemNum %v : Metric has %v labels defined while in expected labels item %v labels defined", mName, itemNum, labelsCount, len(expectedLabelsItem))
			return false
		}
		for _, labelName := range mCfg.Labels {
			if len(expectedLabelsItem[labelName]) == 0 {
				log.Errorf("Invalid expected labels configuration for metric %v, itemNum %v : Metric has label %v defined while in expected labels this label is not defined", mName, itemNum, labelName)
				return false
			}
		}
	}
	return true
}

func (c *Config) initDefaultDB() {
	for dbName, dbConfig := range c.Databases {
		c.DbName = dbName
		c.Db = dbConfig
		break
	}
	log.Infof("DbName is initialized as %v", c.DbName)
}

func processCrypto(path string, config *Config) error {
	if *keyPath == "" {
		log.Info("Key-path is not specified, so read dsn as plain-text")
		for _, dbConfig := range config.Databases {
			dbConfig.DecryptedDsn = dbConfig.Dsn
		}
		return nil
	}

	key, isNew, err := getOrCreateKey()
	if err != nil {
		return fmt.Errorf("Key Error : %+v", err)
	}

	cryptoService, err := crypto.NewCrypto(key)
	if err != nil {
		return fmt.Errorf("Crypto Error : %+v", err)
	}

	configModified := false
	for dbName, dbConfig := range config.Databases {
		if !strings.HasPrefix(dbConfig.Dsn, enc_prefix) {
			dbConfig.DecryptedDsn = dbConfig.Dsn
			encryptedDsn, err := cryptoService.Encrypt([]byte(dbConfig.DecryptedDsn))
			if err != nil {
				log.Errorf("Failed to encrypt dsn for %v : %+v", dbName, err)
			} else {
				dbConfig.Dsn = enc_prefix + encryptedDsn
				log.Infof("Dsn encrypted successfully for %v", dbName)
				configModified = true
			}
		} else {
			if isNew {
				return fmt.Errorf("There is no sense trying to decipher with fresh key %v", *keyPath)
			}
			dbConfig.DecryptedDsn, err = cryptoService.Decrypt([]byte(dbConfig.Dsn[len(enc_prefix):]))
			if err != nil {
				log.Errorf("Failed to decrypt dsn for %v : %+v", dbName, err)
			} else {
				log.Infof("Dsn decrypted successfully for %v", dbName)
			}
		}
	}

	if configModified {
		log.Infof("Modifying %v ...", path)
		err = writeConfigToFile(path, *config)
		if err != nil {
			return fmt.Errorf("Error modifying %v : %+v", path, err)
		} else {
			log.Infof("Config %v modified successfully", path)
		}
	}

	return nil
}

func writeConfigToFile(path string, cfg Config) error {
	err := os.Truncate(path, 0)
	if err != nil {
		return fmt.Errorf("Error truncating file %v : %+v", path, err)
	}
	perms := os.FileMode(utils.GetOctalUintEnvironmentVariable("CONFIG_FILE_PERMS", 0600))
	err = os.Chmod(path, perms)
	if err != nil {
		return fmt.Errorf("Error chmod file %v : %+v", path, err)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND, perms)
	defer func() {
		log.Infof("File %v closed", path)
		_ = f.Close()
	}()
	if err != nil {
		return fmt.Errorf("Error opening file %v : %+v", path, err)
	}
	_, err = f.Seek(0, 0)
	if err != nil {
		return fmt.Errorf("Error executing Seek for file %v : %+v", path, err)
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("Error marshalling yaml : %+v", err)
	}
	_, err = f.Write(out)
	if err != nil {
		return fmt.Errorf("Error writing to file %v : %+v", path, err)
	}
	return nil
}

func getOrCreateKey() (key []byte, new bool, err error) {
	file, err := os.Open(*keyPath)
	defer file.Close()

	if err != nil && !os.IsNotExist(err) {
		return nil, false, err
	}
	buf := bytes.Buffer{}
	length, err := io.Copy(&buf, file)
	if err != nil {
		log.Infof("Error copying key-file %v, probably it doesn't exist : %+v", *keyPath, err)
	} else {
		log.Debugf("Copied %v bytes successfully to the buffer from key-file %v", length, *keyPath)
	}

	encodedKey := buf.String()
	if len(encodedKey) != 0 {
		log.Infof("Key file is not empty. Using key from the file")
		key, err := base64.StdEncoding.DecodeString(encodedKey)
		if err != nil {
			return nil, false, err
		}
		return key, false, nil
	}

	log.Infof("Key file is empty. Generating new key and writing it to %v...", *keyPath)
	newKey, err := randStringBytes(keySize)
	if err != nil {
		return nil, false, fmt.Errorf("Error generating crypto key %+v", err)
	}
	key = []byte(base64.StdEncoding.EncodeToString(newKey))
	file, err = os.Create(*keyPath)
	if err != nil {
		return nil, false, fmt.Errorf("Error creating file %v : %+v", *keyPath, err)
	}
	err = os.Chmod(*keyPath, os.FileMode(utils.GetOctalUintEnvironmentVariable("KEY_FILE_PERMS", 0600)))
	if err != nil {
		return nil, false, fmt.Errorf("Error chmod file %v : %+v", *keyPath, err)
	}
	_, err = file.Write(key)
	if err != nil {
		return nil, false, fmt.Errorf("Error writing file %v : %+v", *keyPath, err)
	}
	log.Infof("Key file generated successfully and written to %v", *keyPath)
	return newKey, true, nil
}

func randStringBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	if err != nil {
		return nil, err
	}
	return b, nil
}
