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

package main

import (
	"log"

	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/backup"
	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/restore"
	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/stanza"
	"github.com/Netcracker/pgskipper-pgbackrest-sidecar/pkg/utils"
	fiber "github.com/gofiber/fiber/v2"
)

func main() {

	go stanza.CreateStanza()
	app := fiber.New()

	app.Post("/backup", func(c *fiber.Ctx) error {
		payload := struct {
			Timestamp string `json:"timestamp"`
		}{}
		if err := c.BodyParser(&payload); err != nil {
			return err
		}
		if err := backup.MakeFullBackup(payload.Timestamp); err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.Status(200).JSON("Backup started successfully")
	})

	app.Post("/backup/diff", func(c *fiber.Ctx) error {
		payload := struct {
			Timestamp string `json:"timestamp"`
		}{}
		if err := c.BodyParser(&payload); err != nil {
			return err
		}

		if err := backup.MakeDiffBackup(payload.Timestamp); err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.Status(200).JSON("Diff backup has been started successfully")
	})

	app.Post("/backup/incr", func(c *fiber.Ctx) error {
		payload := struct {
			Timestamp string `json:"timestamp"`
		}{}
		if err := c.BodyParser(&payload); err != nil {
			return err
		}

		if err := backup.MakeIncrBackup(payload.Timestamp); err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.Status(200).JSON("Delta backup has been started successfully")
	})
	app.Get("/status", func(c *fiber.Ctx) error {
		err, status := backup.GetBackupStatus(c.Query("timestamp"))
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.Status(200).JSON(status)
	})
	app.Get("/list", func(c *fiber.Ctx) error {
		err, status := backup.GetBackupList()
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.Status(200).JSON(status)
	})

	app.Post("/restore", func(c *fiber.Ctx) error {

		payload := utils.Payload{}

		if err := c.BodyParser(&payload); err != nil {
			return err
		}
		err := restore.PerformRestore(payload)
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.Status(200).JSON("Restore procedure has been started successfully")
	})

	app.Post("/upgrade", func(c *fiber.Ctx) error {
		err := stanza.UpgradeStanza()
		if err != nil {
			return c.Status(500).SendString(err.Error())
		}
		return c.Status(200).JSON("Stanza has been upgraded")
	})

	log.Fatal(app.Listen(":3000"))
}
