*** Settings ***
Library           RequestsLibrary
Library           Collections
Library           OperatingSystem
Resource          ../Lib/lib.robot


*** Variables ***
${RETRY_TIME}                       360s
${RETRY_INTERVAL}                   1s


*** Keywords ***
Prepare Test Data
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    Set Suite Variable  ${PG_CLUSTER_NAME}
    ${db_name}  Set Variable  test_terminate_backup_db
    Set Suite Variable  ${db_name}
    Create Database  ${db_name}
    Execute Query  pg-${PG_CLUSTER_NAME}  create table test (id BIGSERIAL NOT NULL, text VARCHAR(200), PRIMARY KEY (id))  ${db_name}
    Execute Query  pg-${PG_CLUSTER_NAME}  insert into test (id, text) select i, md5(random()::text) from generate_series(1, 1000000) s(i);  ${db_name}

Check Backup Status
    [Arguments]  ${backup_id}  ${status}
    ${resp}=  GET On Session   postgres_backup_daemon  url=/backup/status/${backup_id}
    ${resp_status}=  Get From Dictionary  ${resp.json()}  status
    Should Be Equal As Strings  ${resp_status}  ${status}

Check Authorization
    ${PGSSLMODE}=  Get Environment Variable  PGSSLMODE
    ${scheme}=  Set Variable If  '${PGSSLMODE}' == 'require'  https  http
    Create Session  postgres_backup_daemon  ${scheme}://postgres-backup-daemon:9000
    ${resp}=  POST On Session  postgres_backup_daemon  /restore/request  expected_status=401
    Should Be Equal  ${resp.status_code}  ${401}

Prepare Auth
    ${POSTGRES_USER}=  Get Environment Variable  POSTGRES_USER  default=postgres
    ${PG_ROOT_PASSWORD}=  Get Environment Variable  PG_ROOT_PASSWORD
    ${auth}=  Create List  ${POSTGRES_USER}  ${PG_ROOT_PASSWORD}
    reterun  ${auth}
