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
	"fmt"
	"strings"

	"github.com/Netcracker/pgskipper-replication-controller/pkg/postgres"
)

const (
	pubGetQuery                 = "select pubname as name, pubowner::regrole as owner from pg_publication where pubname=$1"
	pubGetTablesQuery           = "select schemaname, tablename, attnames::TEXT, Coalesce(rowfilter,'') from pg_publication_tables where pubname=$1"
	pubCreateAllTablesQuery     = "CREATE publication \"%s\" FOR ALL TABLES;"
	pubCreateWithTablesQuery    = "CREATE publication \"%s\" FOR TABLE %s"
	pubCreateWithSchemasQuery   = "CREATE publication \"%s\" FOR TABLES IN SCHEMA %s"
	pubAlterAddWithTablesQuery  = "ALTER PUBLICATION \"%s\" ADD TABLE %s"
	pubAlterAddWithSchemasQuery = "ALTER PUBLICATION \"%s\" ADD TABLES IN SCHEMA %s"
	pubAlterSetWithTablesQuery  = "ALTER PUBLICATION \"%s\" SET TABLE %s"
	pubDropQuery                = "DROP publication \"%s\";"

	schemasAppend = "TABLES IN SCHEMA"
)

func getPubGetQuery() string {
	return pubGetQuery
}

func getPubGetTablesQuery() string {
	return pubGetTablesQuery
}

func getPubCreateAllTablesQuery(publication string) string {
	return fmt.Sprintf(pubCreateAllTablesQuery, postgres.EscapeInputValue(publication))
}

func getPubCreateQuery(publication string, tables, schemas []string) string {
	return formQueryWithTablesAndSchemas(publication, tables, schemas, pubCreateWithTablesQuery, pubCreateWithSchemasQuery)
}

func getPubAlterAddQuery(publication string, tables, schemas []string) string {
	return formQueryWithTablesAndSchemas(publication, tables, schemas, pubAlterAddWithTablesQuery, pubAlterAddWithSchemasQuery)
}

func getPubAlterSetQuery(publication string, tables []string, schemas []string) string {
	return formQueryWithTablesAndSchemas(publication, tables, schemas, pubAlterSetWithTablesQuery, pubAlterAddWithSchemasQuery)
}

func getPubDropQuery(publication string) string {
	return fmt.Sprintf(pubDropQuery, postgres.EscapeInputValue(publication))
}

func formQueryWithTablesAndSchemas(publication string, tables, schemas []string, queryForTables, queryForSchemas string) string {
	var query string
	areTablesPresent := len(tables) > 0
	areSchemasPresent := len(schemas) > 0
	if areTablesPresent {
		prepTables := prepareTables(tables)
		query = fmt.Sprintf(queryForTables, postgres.EscapeInputValue(publication), strings.Join(prepTables, ","))
	}
	if areSchemasPresent {
		prepSchemas := prepareSchemas(schemas)
		if areTablesPresent {
			query = fmt.Sprintf("%s, %s %s;", query, schemasAppend, strings.Join(prepSchemas, ","))
		} else {
			query = fmt.Sprintf(queryForSchemas, postgres.EscapeInputValue(publication), strings.Join(prepSchemas, ","))
		}
	}
	return query
}

func prepareTables(tables []string) []string {
	preparedTables := make([]string, 0, len(tables))
	for _, origTable := range tables {
		table := postgres.EscapeInputValue(origTable)
		if strings.Contains(table, ".") {
			table = prepareTableWithSchema(table)
		} else {
			table = prepareTableWithArgs(table)
		}
		preparedTables = append(preparedTables, table)
	}
	return preparedTables
}

func prepareTableWithSchema(table string) string {
	tableArr := strings.Split(table, ".")
	schema := fmt.Sprintf("\"%s\"", tableArr[0])
	tableWithArgs := prepareTableWithArgs(tableArr[1])
	table = fmt.Sprintf("%s.%s", schema, tableWithArgs)
	return table
}

func prepareTableWithArgs(table string) string {
	tableWithArgs := strings.Split(table, "(")
	if len(tableWithArgs) > 1 {
		table = fmt.Sprintf("\"%s\"(%s", tableWithArgs[0], tableWithArgs[1])
	} else {
		table = fmt.Sprintf("\"%s\"", table)
	}
	return table
}

func prepareSchemas(schemas []string) []string {
	preparedSchemas := make([]string, 0, len(schemas))
	for _, origSchema := range schemas {
		schema := postgres.EscapeInputValue(origSchema)
		schema = fmt.Sprintf("\"%s\"", schema)
		preparedSchemas = append(preparedSchemas, schema)
	}
	return preparedSchemas
}
