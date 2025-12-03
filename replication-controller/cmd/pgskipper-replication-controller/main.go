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
	"flag"
	"fmt"
	"runtime/debug"
	"strconv"

	"github.com/Netcracker/pgskipper-replication-controller/pkg/postgres"
	publication "github.com/Netcracker/pgskipper-replication-controller/pkg/publicaion"
	"github.com/Netcracker/pgskipper-replication-controller/pkg/users"
	"github.com/Netcracker/pgskipper-replication-controller/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/basicauth"
	"github.com/gofiber/fiber/v2/middleware/recover"
	fiberUtils "github.com/gofiber/fiber/v2/utils"
	"go.uber.org/zap"
)

const (
	pgDB            = "postgres"
	publicationPath = "/publications"
	usersPath       = "/users"

	httpsPort = 8443
)

var (
	pgHost = flag.String("pg_host", utils.GetEnv("POSTGRES_HOST", "127.0.0.1"), "Host of PostgreSQL cluster, env: POSTGRES_HOST")
	pgPort = flag.Int("pg_port", utils.GetEnvInt("POSTGRES_PORT", 5432), "Port of PostgreSQL cluster, env: POSTGRES_PORT")
	pgUser = flag.String("pg_user", utils.GetEnv("POSTGRES_ADMIN_USER", "postgres"), "Username of controller user in PostgreSQL, env: POSTGRES_ADMIN_USER")
	pgPass = flag.String("pg_pass", utils.GetEnv("POSTGRES_ADMIN_PASSWORD", ""), "Password of controller user in PostgreSQL, env: POSTGRES_ADMIN_PASSWORD")
	pgSsl  = flag.String("pg_ssl", utils.GetEnv("PG_SSL", "off"), "Enable ssl connection to postgreSQL, env: PG_SSL")

	servePort = flag.Int("serve_port", 8080, "Port to serve requests incoming to controller")
	serveUser = flag.String(
		"server_user",
		utils.GetEnv("API_USER", "logical-repl-user"),
		"Username to authorize incoming requests, env: API_USER",
	)
	servePass = flag.String(
		"server_pass",
		utils.GetEnv("API_PASSWORD", "logical-repl-password"),
		"Password to authorize incoming requests, env: API_PASSWORD",
	)

	log      = utils.GetLogger()
	pgClient *postgres.Client
)

func main() {
	flag.Parse()
	log.Debug("Controller started")

	app := fiber.New(fiber.Config{Network: "tcp"})

	app.Get("/health", HealthHandler)
	setAuth(app)

	setRecovery(app)

	pgClient = postgres.NewClient(*pgHost, *pgPort, *pgUser, *pgPass, pgDB, *pgSsl)

	pubController := publication.NewPublicationController(pgClient)
	pubGroup := app.Group(publicationPath, func(c *fiber.Ctx) error {
		//Common API Handler
		return c.Next()
	})
	pubGroup.Get("/:database/:publication", pubController.PublicationGetHandler)
	pubGroup.Post("/create", pubController.PublicationCreateHandler)
	pubGroup.Post("/alter/add", pubController.PublicationAlterAddHandler)
	pubGroup.Post("/alter/set", pubController.PublicationAlterSetHandler)
	pubGroup.Delete("/drop", pubController.PublicationDropHandler)

	userController := users.NewUsersController(pgClient)
	usersGroup := app.Group(usersPath, func(c *fiber.Ctx) error {
		//Common API Handler
		return c.Next()
	})
	usersGroup.Post("/grant", userController.GrantUserHandler)

	log.Fatal("Controller has been stopped", zap.Error(RunFiberServer(app)))
}

func setRecovery(app *fiber.App) {
	recoverConfig := recover.ConfigDefault
	recoverConfig.EnableStackTrace = true
	recoverConfig.StackTraceHandler = func(c *fiber.Ctx, e interface{}) {
		log.Error(fmt.Sprintf("Panic: %+v\nStacktrace:\n%s", e, string(debug.Stack())))
	}
	app.Use(recover.New(recoverConfig))
	app.Use(func(c *fiber.Ctx) error {
		// Setting defaults for existed handlers
		c.Request().Header.SetContentType(fiberUtils.GetMIME("json"))
		log.Debug(fmt.Sprintf("%s %s", c.Request().Header.Method(), c.Path()))
		return c.Next()
	})
}

func setAuth(app *fiber.App) {
	app.Use(basicauth.New(basicauth.Config{
		Users: map[string]string{
			*serveUser: *servePass,
		},
	}))
}

func HealthHandler(c *fiber.Ctx) error {
	pgClient.RequestHealth()
	return nil
}

func RunFiberServer(app *fiber.App) error {
	if utils.IsHttpsEnabled() {
		go runServerTLS(app)
	}
	return app.Listen(":" + strconv.Itoa(*servePort))
}

func runServerTLS(app *fiber.App) {
	err := app.ListenTLS(":"+strconv.Itoa(httpsPort), "/certs/tls.crt", "/certs/tls.key")
	if err != nil {
		log.Fatal("error during server execution")
	}
}
