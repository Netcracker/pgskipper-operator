*** Settings ***
Resource          ../Lib/lib.robot
Library           OperatingSystem
Suite Setup       Preparation

*** Variables ***
${user_name}                     test_role
${user_pass}                     SecureP@ssw0rd123
${user_pass_new}                 NewP@ssw0rd456

*** Keywords ***
Preparation
    ${PG_CLUSTER_NAME}=  Get Environment Variable  PG_CLUSTER_NAME  default=patroni
    Set Suite Variable  ${PG_CLUSTER_NAME}


*** Test Cases ***
Check User Creation
    [Tags]  patroni simple  check_user
    Execute Query  pg-${PG_CLUSTER_NAME}  CREATE USER ${user_name};
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT usename FROM pg_user;
    Should Be True   """${user_name}""" in """${res}"""   msg=[creating user] Expected user ${user_name} is not created in pg-${PG_CLUSTER_NAME}: res: ${res}
    [Teardown]  Execute Query  pg-${PG_CLUSTER_NAME}  DROP USER ${user_name};

Check User Creation With Password
    [Tags]  patroni simple  check_user
    Execute Query  pg-${PG_CLUSTER_NAME}  CREATE USER ${user_name} WITH PASSWORD '${user_pass}';
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT usename FROM pg_user;
    Should Be True   """${user_name}""" in """${res}"""   msg=[creating user] Expected user ${user_name} is not created in pg-${PG_CLUSTER_NAME}: res: ${res}
    ${pass}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT passwd from pg_shadow where usename='${user_name}';
    Should Not Be Empty  ${pass}
    [Teardown]  Execute Query  pg-${PG_CLUSTER_NAME}  DROP USER ${user_name};

Check Update User
    [Tags]  patroni simple  check_user
    Execute Query  pg-${PG_CLUSTER_NAME}  CREATE USER ${user_name} WITH PASSWORD '${user_pass}';
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT usename FROM pg_user;
    Should Be True   """${user_name}""" in """${res}"""   msg=[creating user] Expected user ${user_name} is not created in pg-${PG_CLUSTER_NAME}: res: ${res}
    ${previous_pass}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT passwd from pg_shadow where usename='${user_name}';
    Execute Query  pg-${PG_CLUSTER_NAME}  ALTER USER ${user_name} WITH PASSWORD '${user_pass_new}';
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT usename FROM pg_user;
    Should Be True   """${user_name}""" in """${res}"""   msg=[updating user] Expected user ${user_name} is not exist after alter in pg-${PG_CLUSTER_NAME}: res: ${res}
    ${current_pass}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT passwd from pg_shadow where usename='${user_name}';
    Should Not Be Equal  ${previous_pass}  ${current_pass}
    [Teardown]  Execute Query  pg-${PG_CLUSTER_NAME}  DROP USER ${user_name};

Check Deletion Of User
    [Tags]  patroni simple  check_user
    Execute Query  pg-${PG_CLUSTER_NAME}  CREATE USER ${user_name} WITH PASSWORD '${user_pass}';
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT usename FROM pg_user;
    Should Be True   """${user_name}""" in """${res}"""   msg=[creating user] Expected user ${user_name} is not created in pg-${PG_CLUSTER_NAME}: res: ${res}
    Execute Query  pg-${PG_CLUSTER_NAME}  DROP USER ${user_name};
    ${res}=  Execute Query  pg-${PG_CLUSTER_NAME}  SELECT usename FROM pg_user;
    Should Not Be True   """${user_name}""" in """${res}"""   msg=[deleting user] User ${user_name} is not deleted from pg-${PG_CLUSTER_NAME}: res: ${res}
