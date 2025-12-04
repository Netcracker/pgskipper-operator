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

package users

import (
	"context"
	"fmt"

	"github.com/Netcracker/pgskipper-replication-controller/pkg/postgres"
	"github.com/Netcracker/pgskipper-replication-controller/pkg/utils"
	"go.uber.org/zap"
)

type UsersController struct {
	pgClient *postgres.Client
}

func NewUsersController(pgClient *postgres.Client) *UsersController {
	return &UsersController{pgClient: pgClient}
}

func (pc *UsersController) grantUserToReplication(ctx context.Context, request UserRequest) error {
	log := utils.ContextLogger(ctx)
	username := request.Username
	err := validateGrantRequest(username)
	if err != nil {
		log.Error(err.Error(), zap.Error(err))
		return err
	}

	conn, err := pc.pgClient.GetConnection(ctx)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, getGrantReplicationQuery(username))
	if err != nil {
		log.Error(fmt.Sprintf("cannot grant user %s for Replication", username))
		panic(err)
	}
	log.Info(fmt.Sprintf("User %s has been granted for Replication", username))
	return nil
}

func validateGrantRequest(username string) error {
	if len(username) == 0 {
		return fmt.Errorf("username must not be empty")
	}
	return nil
}
