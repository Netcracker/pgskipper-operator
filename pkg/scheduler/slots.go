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

package scheduler

import (
	"fmt"
	"strings"

	pgClient "github.com/Netcracker/pgskipper-operator/pkg/client"
	"github.com/Netcracker/pgskipper-operator/pkg/patroni"
	"github.com/jackc/pgtype"
	"go.uber.org/zap"
)

var (
	ignoredSlots      = make([]Slot, 0)
	ignoreSlotsPrefix = "cdc_rs_"
)

type Slot struct {
	Name     string `json:"name,omitempty"`
	Type     string `json:"type,omitempty"`
	Database string `json:"database,omitempty"`
	Plugin   string `json:"plugin,omitempty"`
}

func updateIgnoredReplicationSlots() error {
	slots, err := getReplicationSlots(pgClient.GetPostgresClient(pgHost))
	if err != nil {
		return err
	}
	if len(slots) < len(ignoredSlots) {
		logger.Info(fmt.Sprintf("New slots list to ignore %v", slots))
		err = updateIgnoreSlotsInConfig(slots, patroniUrl)
		if err != nil {
			return err
		}
		ignoredSlots = slots
		return nil
	}

	difSlots := getNewSlotsForIgnore(slots)
	if len(difSlots) > 0 {
		logger.Info(fmt.Sprintf("New slots to ignore %v", difSlots))
		resultSlots := append(ignoredSlots, difSlots...)
		err = updateIgnoreSlotsInConfig(resultSlots, patroniUrl)
		if err != nil {
			return err
		}
		ignoredSlots = resultSlots
	}
	return nil
}

func getReplicationSlots(client *pgClient.PostgresClient) ([]Slot, error) {
	slots := make([]Slot, 0)
	rows, err := client.Query("select slot_name, plugin, slot_type, database from pg_replication_slots;")
	if err != nil {
		logger.Error("cannot get slots list", zap.Error(err))
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var name pgtype.Text
		var slotType pgtype.Text
		var database pgtype.Text
		var plugin pgtype.Text

		err = rows.Scan(&name, &plugin, &slotType, &database)
		if err != nil {
			logger.Error("error during obtain slot information", zap.Error(err))
			return nil, err
		}
		if !strings.HasPrefix(name.String, ignoreSlotsPrefix) {
			continue
		}
		slot := Slot{
			Name:     name.String,
			Type:     slotType.String,
			Database: database.String,
			Plugin:   plugin.String,
		}

		slots = append(slots, slot)
	}
	return slots, nil
}

func updateIgnoreSlotsInConfig(slots []Slot, patroniUrl string) error {
	if len(slots) == 0 {
		return nil
	}

	patchData := map[string]interface{}{
		"ignore_slots": slots,
	}
	if err := patroni.UpdatePatroniConfig(patchData, patroniUrl); err != nil {
		logger.Error("Failed to patch postgresql params via patroni", zap.Error(err))
		return err
	}

	return nil
}

func getNewSlotsForIgnore(slots []Slot) (difSlots []Slot) {
	for _, slot := range slots {
		exist := false
		for _, ignoredSlot := range ignoredSlots {
			if ignoredSlot.Name == slot.Name {
				exist = true
			}
		}
		if !exist {
			difSlots = append(difSlots, slot)
		}
	}
	return
}
