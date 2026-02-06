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
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	logger = GetLogger()
)

type Payload struct {
	BackupId string `json:"backupId,omitempty"`
	Type     string `json:"type,omitempty"`
	Target   string `json:"target,omitempty"`
}

func ExecCommand(command string, args []string) (error, string) {
	var output string

	logger.Info(fmt.Sprintf("Executed command is %s with args %v", command, args))
	cmd := exec.Command(command, args...)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logger.Error("Error obtaining StdoutPipe", zap.Error(err))
		return err, ""
	}
	if err := cmd.Start(); err != nil {
		logger.Error("Error starting command", zap.Error(err))
		return err, ""
	}

	in := bufio.NewScanner(stdout)

	for in.Scan() {
		output = in.Text()
		logger.Info(output)

	}
	if err = in.Err(); err != nil {
		logger.Error("Error reading stdout", zap.Error(err))
		return err, ""
	}

	if err := cmd.Wait(); err != nil {
		logger.Error("Error waiting for command to finish", zap.Error(err), zap.String("stderr", stderr.String()))
		return err, ""
	}

	return nil, output
}

func GetLogger() *zap.Logger {
	atom := zap.NewAtomicLevel()
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	logger := zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atom,
	))
	defer func() { _ = logger.Sync() }()
	return logger
}
