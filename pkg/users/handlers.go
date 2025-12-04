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
	"github.com/Netcracker/pgskipper-replication-controller/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type UserRequest struct {
	Username string `json:"username"`
}

func (pc *UsersController) GrantUserHandler(c *fiber.Ctx) error {
	request, err := getUserReq(c)
	if err != nil {
		return err
	}
	ctx := utils.GetRequestContext(c)
	err = pc.grantUserToReplication(ctx, request)
	if err != nil {
		return badReq(c, err)
	}
	return ok(c)
}

func getUserReq(c *fiber.Ctx) (UserRequest, error) {
	var request UserRequest
	if len(c.Body()) > 0 {
		err := c.BodyParser(&request)
		if err != nil {
			return request, err
		}
	}
	return request, nil
}

func badReq(c *fiber.Ctx, err error) error {
	return c.Status(fiber.StatusBadRequest).SendString(err.Error())
}

func ok(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).SendString("OK")
}
