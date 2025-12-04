*** Settings ***
Resource           ../Lib/lib.robot

*** Keywords ***
Backup Daemon Is Working
    Backup Daemon Alive
    WAL Archiving is Enabled
    ${NUMBER_OF_WALS}=  Get Number Of Stored WAL Archives
    Set Global Variable   ${NUMBER_OF_WALS}

WAL Archive Switched
    Switch WAL Archive

New WAL Files Are Presented On Storage
    Number Of WALs Increased    ${NUMBER_OF_WALS}

*** Test Cases ***
Check That After Switching Of WAL File, File Exists In Backup Storage
    [Tags]   backup full   Stability-tests   check_wal_archiving
    Given Backup Daemon Is Working
    When WAL Archive Switched
    Then New WAL Files Are Presented On Storage
