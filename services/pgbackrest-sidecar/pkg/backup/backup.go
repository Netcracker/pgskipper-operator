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
	"encoding/json"
	"fmt"
	"os"

	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/utils"
	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/utils/constants"
	"go.uber.org/zap"
)

var (
	logger = utils.GetLogger()
)

type Root struct {
	Backup []Backup `json:"backup"`
	Status Status   `json:"status"`
}
type Backup struct {
	Annotation map[string]string `json:"annotation"`
	Error      bool              `json:"error"`
	Info       Info              `json:"info"`
	Label      string            `json:"label"`
	Type       string            `json:"type"`
}

type Info struct {
	Delta int `json:"delta"`
	Size  int `json:"size"`
}

type LockBackup struct {
	Held bool `json:"held"`
}

type Lock struct {
	Backup LockBackup `json:"backup"`
}
type Status struct {
	Code    int    `json:"code"`
	Lock    Lock   `json:"lock"`
	Message string `json:"message"`
}

func MakeFullBackup(timestamp string) error {
	annotation := fmt.Sprintf("--annotation=timestamp=%s", timestamp)
	args := []string{fmt.Sprintf("--stanza=%s", os.Getenv("PGBACKREST_STANZA")), "--repo=1", annotation}
	args = append(args, "--type=full", "backup")
	logger.Info("Backup has been started")
	if err, _ := utils.ExecCommand(constants.BackrestBin, args); err != nil {

		return err
	}
	return nil
}

func MakeDiffBackup(timestamp string) error {
	annotation := fmt.Sprintf("--annotation=timestamp=%s", timestamp)
	args := []string{fmt.Sprintf("--stanza=%s", os.Getenv("PGBACKREST_STANZA")), "--repo=1", annotation}
	args = append(args, "--type=diff", "backup")
	logger.Info("Diff Backup has been started")
	if err, _ := utils.ExecCommand(constants.BackrestBin, args); err != nil {

		return err
	}
	return nil
}

func MakeIncrBackup(timestamp string) error {
	annotation := fmt.Sprintf("--annotation=timestamp=%s", timestamp)
	args := []string{fmt.Sprintf("--stanza=%s", os.Getenv("PGBACKREST_STANZA")), "--repo=1", annotation}
	args = append(args, "--type=incr", "backup")
	logger.Info("Incremental Backup has been started")
	if err, _ := utils.ExecCommand(constants.BackrestBin, args); err != nil {

		return err
	}
	return nil
}

func GetBackupStatus(label string) (error, Backup) {
	var root []Root
	args := []string{"info", "--output=json", fmt.Sprintf("--stanza=%s", os.Getenv("PGBACKREST_STANZA"))}
	logger.Info("Backup status requested")
	err, output := utils.ExecCommand(constants.BackrestBin, args)
	if err != nil {
		return err, Backup{}
	}
	if err := json.Unmarshal([]byte(output), &root); err != nil {
		logger.Error("Error while unmarshalling output", zap.Error(err))
		return err, Backup{}
	}
	logger.Info(fmt.Sprintf("info %v", root))
	status := GetBackupIdStatus(label, root)
	return nil, status
}

func GetBackupIdStatus(label string, backupList []Root) Backup {
	for _, item := range backupList {
		logger.Info(fmt.Sprintf("Item %v", item))
		for _, backup := range item.Backup {
			logger.Info(fmt.Sprintf("Backup %v", backup))
			if timestamp, ok := backup.Annotation["timestamp"]; ok {
				logger.Info(fmt.Sprintf("Compare %v and %v", label, timestamp))
				if label == timestamp {
					return backup
				}
			}
		}

	}
	return Backup{}
}

func GetBackupList() (error, []Backup) {
	var root []Root
	args := []string{"info", "--output=json", fmt.Sprintf("--stanza=%s", os.Getenv("PGBACKREST_STANZA"))}
	logger.Info("Backup list requested")
	err, output := utils.ExecCommand(constants.BackrestBin, args)
	if err != nil {
		return err, []Backup{}
	}
	if err := json.Unmarshal([]byte(output), &root); err != nil {
		logger.Error("Error while unmarshalling output", zap.Error(err))
		return err, []Backup{}
	}
	return nil, root[0].Backup
}
