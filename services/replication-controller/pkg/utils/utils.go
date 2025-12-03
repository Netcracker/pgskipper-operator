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

package utils

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	isDebugEnabled = *flag.Bool("log_debug", GetEnvBool("LOG_DEBUG", false), "If debug logs is enabled, env: LOG_DEBUG")
	log            *zap.Logger
)

type RequestId string

func init() {
	log = GetLogger()
}

func GetLogger() *zap.Logger {
	atom := zap.NewAtomicLevel()
	if isDebugEnabled {
		atom = zap.NewAtomicLevelAt(zap.DebugLevel)
	}

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	))
	defer func() {
		_ = logger.Sync()
	}()
	return logger
}

func GetEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

func GetEnvInt(key string, fallback int) int {
	if value, ok := os.LookupEnv(key); ok {
		if ivalue, err := strconv.Atoi(value); err == nil {
			return ivalue
		}
	}
	return fallback
}

func GetEnvBool(key string, fallback bool) bool {
	if value, ok := os.LookupEnv(key); ok {
		bvalue, err := strconv.ParseBool(value)
		if err != nil {
			log.Error(fmt.Sprintf("Can't parse %s boolean variable", key), zap.Error(err))
			panic(err)
		}
		return bvalue
	}
	return fallback
}

func ContextLogger(ctx context.Context) *zap.Logger {
	logger := GetLogger()
	return logger.With(zap.ByteString("request_id", []byte(fmt.Sprintf("%s", ctx.Value(RequestId("request_id"))))))
}

func GetRequestContext(c *fiber.Ctx) context.Context {
	requestId := c.Request().Header.Peek("X-Request-ID")
	if len(requestId) == 0 {
		id := uuid.New().String()
		c.Set("X-Request-ID", id)
		requestId = []byte(id)
	}

	bg := context.Background()
	ctx := context.WithValue(bg, RequestId("request_id"), requestId)
	return ctx
}

func IsHttpsEnabled() bool {
	return GetEnv("TLS_ENABLED", "false") == "true"
}
