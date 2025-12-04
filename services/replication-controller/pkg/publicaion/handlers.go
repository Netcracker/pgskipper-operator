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

package publication

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Netcracker/pgskipper-replication-controller/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"go.uber.org/zap"
)

type CommonRequest struct {
	PubName  string   `json:"publicationName"`
	Database string   `json:"database"`
	Tables   []string `json:"tables,omitempty"`
	Schemas  []string `json:"schemas,omitempty"`
}

func (pc *PublicationController) PublicationCreateHandler(c *fiber.Ctx) error {
	return handleCommonFunc(c, pc.createPublication)
}

func (pc *PublicationController) PublicationAlterAddHandler(c *fiber.Ctx) error {
	return handleCommonFunc(c, pc.alterAddPublication)
}

func (pc *PublicationController) PublicationAlterSetHandler(c *fiber.Ctx) error {
	return handleCommonFunc(c, pc.alterSetPublication)
}

func (pc *PublicationController) PublicationDropHandler(c *fiber.Ctx) error {
	return handleCommonFunc(c, pc.dropPublication)
}

func (pc *PublicationController) PublicationGetHandler(c *fiber.Ctx) error {
	request := CommonRequest{
		Database: c.Params("database"),
		PubName:  c.Params("publication"),
	}

	withTables, err := getQueryBoolParam(c, "withTables")
	if err != nil {
		return badReq(c, err)
	}

	ctx := utils.GetRequestContext(c)
	pubInfo, err := pc.getPublication(ctx, request, withTables)
	if err != nil {
		if err == isNotFoundErr {
			return c.SendStatus(fiber.StatusNotFound)
		}
		return badReq(c, err)
	}

	return c.Status(fiber.StatusOK).JSON(pubInfo)
}

func handleCommonFunc(c *fiber.Ctx, handleFunc func(context.Context, CommonRequest) error) error {
	request, err := getCommonReq(c)
	if err != nil {
		return err
	}
	ctx := utils.GetRequestContext(c)
	err = handleFunc(ctx, request)
	if err != nil {
		return badReq(c, err)
	}
	return ok(c)
}

func getCommonReq(c *fiber.Ctx) (CommonRequest, error) {
	var request CommonRequest
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

func getQueryBoolParam(c *fiber.Ctx, param string) (bool, error) {
	paramStr := c.Query(param, "false")
	boolVal, err := strconv.ParseBool(paramStr)
	if err != nil {
		log.Error(fmt.Sprintf("cannot parse bool value for param %s", param), zap.Error(err))
		return false, err
	}
	return boolVal, nil
}
