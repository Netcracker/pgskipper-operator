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
	"strings"

	"github.com/Netcracker/pgskipper-replication-controller/pkg/postgres"
	"github.com/Netcracker/pgskipper-replication-controller/pkg/utils"
	"github.com/jackc/pgx/v4"
	"go.uber.org/zap"
)

var (
	isNotFoundErr = pgx.ErrNoRows
)

type PublicationController struct {
	pgClient *postgres.Client
}

type PublicationInfo struct {
	Name     string             `json:"name"`
	Owner    string             `json:"owner"`
	Database string             `json:"database"`
	Tables   map[string][]Table `json:"tables,omitempty"`
}

type Table struct {
	Name      string   `json:"name"`
	Attr      []string `json:"attrNames"`
	RowFilter string   `json:"rowfilter,omitempty"`
}

func NewPublicationController(pgClient *postgres.Client) *PublicationController {
	return &PublicationController{pgClient: pgClient}
}

func (pc *PublicationController) getPublication(ctx context.Context, request CommonRequest, withTables bool) (PublicationInfo, error) {
	log := utils.ContextLogger(ctx)

	database := request.Database
	publication := request.PubName
	err := validatePublication(publication, database)
	if err != nil {
		log.Error(err.Error(), zap.Error(err))
		return PublicationInfo{}, err
	}
	return pc.getPublicationInternal(ctx, publication, database, withTables)
}

// Error is only isNotFoundErr
func (pc *PublicationController) getPublicationInternal(ctx context.Context, publication, database string, withTables bool) (PublicationInfo, error) {
	log := utils.ContextLogger(ctx)

	log.Info(fmt.Sprintf("Get publication %s for database %s", publication, database))
	conn, err := pc.pgClient.GetConnectionToDb(ctx, database)
	if err != nil {
		if strings.Contains(err.Error(), "(SQLSTATE 3D000)") {
			return PublicationInfo{}, isNotFoundErr
		}
		panic(err)
	}
	defer conn.Close(ctx)

	pubInfo := PublicationInfo{}
	rows, err := conn.Query(ctx, getPubGetQuery(), publication)
	if err != nil {
		log.Error(fmt.Sprintf("cannot get publication %s for database %s", publication, database))
		panic(err)
	}
	defer rows.Close()

	if rows.Next() {
		err = rows.Scan(&pubInfo.Name, &pubInfo.Owner)
		if err != nil {
			log.Error(fmt.Sprintf("cannot scan publication %s for database %s", publication, database))
			panic(err)
		}
	} else {
		return PublicationInfo{}, isNotFoundErr
	}
	pubInfo.Database = database

	// Fill tables info
	if withTables {
		rows.Close()
		rows, err := conn.Query(ctx, getPubGetTablesQuery(), publication)
		if err != nil {
			log.Error(fmt.Sprintf("cannot get publication %s tables info for database %s", publication, database))
			panic(err)
		}

		tables, err := processTableRows(rows)
		if err != nil {
			log.Error(fmt.Sprintf("cannot scan publication %s tables info for database %s", publication, database))
			panic(err)
		}
		pubInfo.Tables = tables
	}

	log.Info(fmt.Sprintf("Publication %s has been get for database %s", publication, database))
	return pubInfo, nil
}

func (pc *PublicationController) createPublication(ctx context.Context, request CommonRequest) error {
	log := utils.ContextLogger(ctx)

	database := request.Database
	publication := request.PubName
	err := validatePublication(publication, database)
	if err != nil {
		log.Error(err.Error(), zap.Error(err))
		return err
	}
	log.Info(fmt.Sprintf("Publication %s creation started for database %s", publication, database))
	if pc.isPublicationExists(ctx, publication, database) {
		log.Info(fmt.Sprintf("Publication %s already exists in database %s", publication, database))
		return nil
	}

	conn, err := pc.pgClient.GetConnectionToDb(ctx, database)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	tables := request.Tables
	schemas := request.Schemas
	if len(tables) == 0 && len(schemas) == 0 {
		log.Debug(getPubCreateQuery(publication, tables, schemas))
		_, err = conn.Exec(ctx, getPubCreateAllTablesQuery(publication))
		if err != nil {
			log.Error(fmt.Sprintf("cannot create publication %s for database %s", publication, database))
			panic(err)
		}
	} else {
		log.Debug(getPubCreateQuery(publication, tables, schemas))
		_, err = conn.Exec(ctx, getPubCreateQuery(publication, tables, schemas))
		if err != nil {
			log.Error(fmt.Sprintf("cannot create publication %s for database %s for tables %s", publication, database, tables))
			panic(err)
		}
	}

	log.Info(fmt.Sprintf("Publication %s has been created for database %s", publication, database))
	return nil
}

func (pc *PublicationController) alterAddPublication(ctx context.Context, request CommonRequest) error {
	log := utils.ContextLogger(ctx)

	database := request.Database
	publication := request.PubName
	err := validatePublication(publication, database)
	if err != nil {
		log.Error(err.Error(), zap.Error(err))
		return err
	}
	log.Info(fmt.Sprintf("Publication %s alter add started for database %s", publication, database))
	if !pc.isPublicationExists(ctx, publication, database) {
		log.Info(fmt.Sprintf("Publication %s doesn't exist in database %s", publication, database))
		return isNotFoundErr
	}

	tables := request.Tables
	schemas := request.Schemas
	if len(tables) == 0 && len(schemas) == 0 {
		errMsg := fmt.Sprintf("Nothing to add to publication %s in database %s", publication, database)
		log.Error(errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	conn, err := pc.pgClient.GetConnectionToDb(ctx, database)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	log.Debug(getPubAlterAddQuery(publication, tables, schemas))
	_, err = conn.Exec(ctx, getPubAlterAddQuery(publication, tables, schemas))
	if err != nil {
		log.Error(fmt.Sprintf("cannot alter add publication %s for database %s", publication, database), zap.Error(err))
		if strings.Contains(err.Error(), "(SQLSTATE 42710)") {
			return err
		}
		panic(err)
	}

	log.Info(fmt.Sprintf("Publication %s has been altered for database %s", publication, database))
	return nil
}

func (pc *PublicationController) alterSetPublication(ctx context.Context, request CommonRequest) error {
	log := utils.ContextLogger(ctx)

	database := request.Database
	publication := request.PubName
	err := validatePublication(publication, database)
	if err != nil {
		log.Error(err.Error(), zap.Error(err))
		return err
	}
	log.Info(fmt.Sprintf("Publication %s alter set started for database %s", publication, database))
	if !pc.isPublicationExists(ctx, publication, database) {
		log.Info(fmt.Sprintf("Publication %s doesn't exist in database %s", publication, database))
		return isNotFoundErr
	}

	tables := request.Tables
	schemas := request.Schemas
	if len(tables) == 0 && len(schemas) == 0 {
		errMsg := fmt.Sprintf("Nothing to add to publication %s in database %s", publication, database)
		log.Error(errMsg)
		return fmt.Errorf("%s", errMsg)
	}

	conn, err := pc.pgClient.GetConnectionToDb(ctx, database)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	log.Debug(getPubAlterSetQuery(publication, tables, schemas))
	_, err = conn.Exec(ctx, getPubAlterSetQuery(publication, tables, schemas))
	if err != nil {
		log.Error(fmt.Sprintf("cannot alter set publication %s for database %s", publication, database), zap.Error(err))
		if strings.Contains(err.Error(), "(SQLSTATE 42710)") {
			return err
		}
		panic(err)
	}

	log.Info(fmt.Sprintf("Publication %s has been altered for database %s", publication, database))
	return nil
}

func (pc *PublicationController) dropPublication(ctx context.Context, request CommonRequest) error {
	log := utils.ContextLogger(ctx)

	publication := request.PubName
	database := request.Database
	err := validatePublication(publication, database)
	if err != nil {
		log.Error(err.Error(), zap.Error(err))
		return err
	}

	log.Info(fmt.Sprintf("Publication %s drop started for database %s", publication, database))
	if !pc.isPublicationExists(ctx, publication, database) {
		log.Info(fmt.Sprintf("Publication %s doesn't exist in database %s", publication, database))
		return nil
	}

	conn, err := pc.pgClient.GetConnectionToDb(ctx, database)
	if err != nil {
		panic(err)
	}
	defer conn.Close(ctx)

	log.Debug(getPubDropQuery(publication))
	_, err = conn.Exec(ctx, getPubDropQuery(publication))
	if err != nil {
		log.Error(fmt.Sprintf("cannot drop publication %s for database %s", publication, database))
		panic(err)
	}
	log.Info(fmt.Sprintf("Publication %s has been dropped for database %s", publication, database))
	return nil
}

func processTableRows(rows pgx.Rows) (map[string][]Table, error) {
	defer rows.Close()

	tablesInfo := make(map[string][]Table)
	var schema string
	var table Table
	var attrStr string

	for rows.Next() {
		err := rows.Scan(&schema, &table.Name, &attrStr, &table.RowFilter)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		table.Attr = convAttrStrToSlice(attrStr)
		if tables, ok := tablesInfo[schema]; ok {
			tables = append(tables, table)
			tablesInfo[schema] = tables
		} else {
			tables = []Table{table}
			tablesInfo[schema] = tables
		}
	}
	return tablesInfo, nil
}

func (pc *PublicationController) isPublicationExists(ctx context.Context, publication, database string) bool {
	_, err := pc.getPublicationInternal(ctx, publication, database, false)
	return err != isNotFoundErr
}

func validatePublication(publication, database string) error {
	if len(database) == 0 {
		return fmt.Errorf("database must not be empty")
	}
	if len(publication) == 0 {
		return fmt.Errorf("database must not be empty")
	}
	return nil
}

func convAttrStrToSlice(attrs string) []string {
	attrs = strings.Trim(attrs, "{")
	attrs = strings.Trim(attrs, "}")
	return strings.Split(attrs, ",")
}
