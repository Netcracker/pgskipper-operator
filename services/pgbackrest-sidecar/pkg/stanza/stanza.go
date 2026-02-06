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

package stanza

import (
	"fmt"
	"time"

	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/utils"
	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/utils/constants"
)

var (
	args   = []string{"stanza-create", "--log-level-console=info"}
	logger = utils.GetLogger()
)

func CreateStanza() error {
	timeout := time.After(30 * time.Minute)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		if err, _ := utils.ExecCommand("pg_isready", []string{"-q"}); err != nil {
			logger.Error(fmt.Sprintf("Postgres is not ready yet [%v], waiting...", err))
		} else if err, _ := utils.ExecCommand(constants.BackrestBin, args); err != nil {
			logger.Error(fmt.Sprintf("While creating stanza an error occurred %v", err))
			return err
		} else {
			return nil
		}

		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for postgres to be ready, stanza create stopped")
		case <-ticker.C:
			// continue loop
		}
	}
}

func UpgradeStanza() error {
	cmd := []string{"stanza-upgrade", "--log-level-console=info"}
	if err, _ := utils.ExecCommand(constants.BackrestBin, cmd); err != nil {
		logger.Error(fmt.Sprintf("While creating stanza en error occures %v", err))
		return err
	}
	return nil

}
