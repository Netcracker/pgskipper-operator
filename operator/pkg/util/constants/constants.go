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

package constants

const (
	ArchiveModeOff           = "off"
	ArchiveModeOn            = "on"
	PasswordEncryption       = "md5"
	ArchiveCommand           = `/opt/scripts/archive_wal.sh "%p" "%f"`
	PgBackRestArchiveCommand = `pgbackrest --stanza=patroni archive-push "%p"`
	PgBackRestRestoreCommand = `pgbackrest --stanza=patroni archive-get "%f" "%p"`
	RestoreCommand           = `curl -u postgres:"\${PG_ROOT_PASSWORD}" -v -S -f --connect-timeout 3 --speed-time 30 --speed-limit 100 postgres-backup-daemon:8082/archive/get?filename=%f -o %p`
	TelegrafJsonKey          = "telegraf-plugin-ura.json"
	PostgreSQLPort           = 5432
	CloudSQL                 = "cloudsql"
	RDS                      = "rds"
	Azure                    = "azure"
)

var PgHba = []string{
	"local   all             postgres                                trust",
	"host    all             postgres             127.0.0.1/32       trust",
	"host    all             postgres             ::1/128            trust",
	"local   replication     all                                     trust",
	"host    replication     all                  127.0.0.1/32       trust",
	"host    replication     all                  ::1/128            trust",
	"host    replication     replicator           0.0.0.0/0          md5",
	"host    replication     postgres             0.0.0.0/0          md5",
	"host    all             all                  0.0.0.0/0          md5",
	"local   all             all                                     md5",
	"host    replication     replicator           ::0/0              md5",
	"host    replication     postgres             ::0/0              md5",
	"host    all             all                  ::0/0              md5",
}
