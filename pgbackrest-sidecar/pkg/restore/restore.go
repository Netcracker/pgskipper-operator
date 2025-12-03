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

package restore

import (
	"fmt"
	"os"

	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/utils"
	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/utils/constants"
)

var (
	logger = utils.GetLogger()
)

func setParams(payload utils.Payload) []string {
	var args []string
	if payload.BackupId != "" {
		value := fmt.Sprintf("--set=%s", payload.BackupId)
		args = append(args, value)
	}
	if payload.Type != "" {
		value := fmt.Sprintf("--type=%s", payload.Type)
		args = append(args, value)
	}
	if payload.Target != "" {
		value := fmt.Sprintf("--target=%s", payload.Target)
		args = append(args, value)
	}
	return args
}

func PerformRestore(payload utils.Payload) error {
	restoreParams := setParams(payload)
	args := []string{"restore", fmt.Sprintf("--stanza=%s", os.Getenv("PGBACKREST_STANZA"))}
	args = append(args, restoreParams...)
	logger.Info("Restore procedure has been started")
	if err, _ := utils.ExecCommand(constants.BackrestBin, args); err != nil {

		return err
	}
	return nil
}
