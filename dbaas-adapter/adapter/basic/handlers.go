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

package basic

import (
	"context"
	"fmt"

	"github.com/Netcracker/pgskipper-dbaas-adapter/postgresql-dbaas-adapter/adapter/util"
	"github.com/gofiber/fiber/v2"
	"go.uber.org/zap"
)

func (sa ServiceAdapter) UpdatePostgreSQLSettingsHandler() func(c *fiber.Ctx) error {
	return func(c *fiber.Ctx) error {
		sa.log.Info("Received request to update settings")
		dbName := c.Params("dbName")

		if !validateDbIdentifierParam(context.Background(), "dbName", dbName, DbIdentifiersPattern) {
			return sendInvalidParameterResponse(c, "dbName", dbName, DbIdentifiersPattern)
		}

		var updateSettingsRequest PostgresUpdateSettingsRequest
		err := c.BodyParser(&updateSettingsRequest)
		if err != nil {
			sa.log.Error("Failed to parse request in update settings handler", zap.Error(err))
			return c.Status(500).SendString(err.Error())
		}
		updateResult, err := sa.updateSettings(dbName, updateSettingsRequest)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}

		if updateResult.isStatusExists(Failed) {
			return c.Status(500).JSON(updateResult)
		}

		if updateResult.isStatusExists(BadRequest) {
			return c.Status(400).JSON(updateResult)
		}

		return c.JSON(updateResult)
	}
}

func (sa ServiceAdapter) GetDatabaseOwnerHandler() func(c *fiber.Ctx) error {
	ctx := context.Background()
	logger := util.ContextLogger(ctx)
	return func(c *fiber.Ctx) error {
		logger.Info("Received request to get database info")
		dbName := c.Params("dbName")

		if dbName == "" || !validateDbIdentifierParam(ctx, "dbName", dbName, DbIdentifiersPattern) {
			return sendInvalidParameterResponse(c, "dbName", dbName, DbIdentifiersPattern)
		}

		userName, err := sa.getDBInfo(ctx, dbName)
		if err != nil {
			return c.Status(500).SendString(fmt.Sprintf("Cannot get info for %s", dbName))
		}
		return c.JSON(userName)
	}
}

func sendInvalidParameterResponse(c *fiber.Ctx, paramName string, paramValue string, pattern string) error {
	return c.Status(400).SendString(fmt.Sprintf("Invalid '%s' param provided: %s. '%s' param must comply to the pattern %s", paramName, paramValue, paramName, pattern))
}

func (sr PostgresUpdateSettingsResult) isStatusExists(status string) bool {
	for _, result := range sr.SettingUpdateResult {
		if result.Status == status {
			return true
		}
	}
	return false
}
