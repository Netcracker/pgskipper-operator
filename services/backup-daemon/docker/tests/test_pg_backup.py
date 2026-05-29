# Copyright 2024-2025 NetCracker Technology Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""
Tests for subprocess pipeline safety in PostgreSQLDumpWorker.

Covers the Step-1 hardening changes:
 - shell=True removed from all Popen calls
 - openssl key passed as a separate list element (not interpolated into a shell string)
 - fetch_roles and dump_roles_for_db pipelines rebuilt as chained Popen calls
"""

import os
import sys
import threading
import unittest
from unittest.mock import MagicMock, patch, mock_open

sys.path.insert(0, os.path.join(os.path.dirname(__file__), '..', 'granular'))

# ---------------------------------------------------------------------------
# Pre-mock every import that pg_backup pulls in so we can import the module
# under test without a running PostgreSQL cluster or installed apt packages.
# ---------------------------------------------------------------------------
for _mod in ('psycopg2', 'storage_s3', 'encryption', 'configs', 'backups', 'utils'):
    sys.modules.setdefault(_mod, MagicMock())

import pg_backup  # noqa: E402  (must come after sys.modules patching)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _mock_proc(returncode=0, stdout_data=b''):
    proc = MagicMock()
    proc.returncode = returncode
    proc.stdout = MagicMock()
    proc.wait.return_value = returncode
    proc.communicate.return_value = (stdout_data, b'')
    return proc


def _make_worker(key='testkey', encryption=True, parallel_jobs='1',
                 bin_path='/usr/lib/postgresql/14/bin'):
    """Construct a PostgreSQLDumpWorker with attributes set directly, bypassing __init__."""
    worker = object.__new__(pg_backup.PostgreSQLDumpWorker)
    threading.Thread.__init__(worker)
    worker.log = MagicMock()
    worker.backup_id = 'bkp-001'
    worker.name = 'bkp-001'
    worker.namespace = 'default'
    worker.compression_level = 9
    worker.postgres_version = [14, 5]
    worker.is_standard_storage = True
    worker.location = '/backup-storage/pg14/granular'
    worker.external_backup_root = None
    worker.bin_path = bin_path
    worker.parallel_jobs = parallel_jobs
    worker.databases = []
    worker.blob_path = None
    worker.backup_dir = '/tmp/bkp-001'
    worker.s3 = None
    worker._cancel_event = threading.Event()
    worker.encryption = encryption
    if encryption:
        worker.key = key
        worker.key_name = 'kname'
        worker.key_source = 'vault'
    worker.storage_name = ''
    worker.status = {
        'backupId': 'bkp-001',
        'namespace': 'default',
        'status': 'Planned',
        'storageName': '',
    }
    worker.pg_dump_proc = None
    return worker


def _setup_configs():
    """Populate mock return values for configs calls used by backup_single_database."""
    pg_backup.configs.postgresql_user.return_value = 'postgres'
    pg_backup.configs.postgresql_host.return_value = 'localhost'
    pg_backup.configs.postgresql_port.return_value = '5432'
    pg_backup.configs.is_external_pg.return_value = False
    pg_backup.configs.postgres_password.return_value = None
    pg_backup.configs.postgresql_no_role_password_flag.return_value = ''
    pg_backup.configs.protected_roles.return_value = []
    pg_backup.configs.connection_properties.return_value = {
        'host': 'localhost', 'port': '5432', 'user': 'postgres',
        'password': None, 'database': 'postgres', 'connect_timeout': 5,
    }


def _setup_psycopg2_mock():
    """Make psycopg2.connect return a mock conn whose cursor yields empty rows."""
    cursor_mock = MagicMock()
    cursor_mock.fetchall.return_value = []
    conn_mock = MagicMock()
    conn_mock.cursor.return_value.__enter__ = MagicMock(return_value=cursor_mock)
    conn_mock.cursor.return_value.__exit__ = MagicMock(return_value=False)
    pg_backup.psycopg2.connect.return_value = conn_mock
    return conn_mock


# ---------------------------------------------------------------------------
# Tests
# ---------------------------------------------------------------------------

class TestNoShellTrue(unittest.TestCase):
    """Static guard: the source must contain zero shell=True occurrences."""

    def test_source_has_no_shell_true(self):
        src = os.path.join(os.path.dirname(__file__), '..', 'granular', 'pg_backup.py')
        with open(src) as f:
            content = f.read()
        self.assertNotIn('shell=True', content,
                         "Found shell=True in pg_backup.py — command injection risk")


class TestBackupSingleDatabaseEncryption(unittest.TestCase):
    """backup_single_database: openssl Popen uses shell=False and a list of args."""

    def _run(self, key, parallel_jobs='1'):
        _setup_configs()
        worker = _make_worker(key=key, encryption=True, parallel_jobs=parallel_jobs)
        def _fake_update(k, v, database=None, flush=False):
            worker.status[k] = v
        worker.update_status = MagicMock(side_effect=_fake_update)
        worker.fetch_roles = MagicMock(return_value='')
        worker.dump_roles_for_db = MagicMock()

        pg_backup.backups.build_backup_path.return_value = '/tmp/bkp-001'
        pg_backup.backups.build_database_backup_path.return_value = '/tmp/testdb.dump'
        pg_backup.utils.get_owner_of_db.return_value = 'postgres'

        pg_dump_mock = _mock_proc()
        openssl_mock = _mock_proc()

        with patch('subprocess.Popen', side_effect=[pg_dump_mock, openssl_mock]) as mock_popen, \
             patch('builtins.open', mock_open()):
            worker.backup_single_database('testdb')

        return mock_popen.call_args_list

    # --- single-job path --------------------------------------------------

    def test_single_job_openssl_cmd_is_list(self):
        calls = self._run('testkey', parallel_jobs='1')
        cmd = calls[1][0][0]
        self.assertIsInstance(cmd, list, "openssl cmd must be a list, not a shell string")

    def test_single_job_openssl_shell_false(self):
        calls = self._run('testkey', parallel_jobs='1')
        self.assertFalse(calls[1][1].get('shell', False))

    def test_single_job_key_is_separate_list_element(self):
        calls = self._run('testkey', parallel_jobs='1')
        cmd = calls[1][0][0]
        pass_idx = cmd.index('-pass')
        self.assertEqual(cmd[pass_idx + 1], 'pass:testkey')

    def test_single_job_key_with_shell_metacharacters(self):
        dangerous_key = 'secret;rm -rf /'
        calls = self._run(dangerous_key, parallel_jobs='1')
        cmd = calls[1][0][0]
        pass_arg = cmd[cmd.index('-pass') + 1]
        self.assertEqual(pass_arg, 'pass:{}'.format(dangerous_key),
                         "Key with shell metacharacters must be passed verbatim")

    def test_single_job_key_with_subshell_syntax(self):
        dangerous_key = 'key$(touch /tmp/pwned)'
        calls = self._run(dangerous_key, parallel_jobs='1')
        cmd = calls[1][0][0]
        pass_arg = cmd[cmd.index('-pass') + 1]
        self.assertEqual(pass_arg, 'pass:{}'.format(dangerous_key))

    # --- parallel-jobs path -----------------------------------------------

    def test_parallel_job_openssl_cmd_is_list(self):
        calls = self._run('testkey', parallel_jobs='2')
        cmd = calls[1][0][0]
        self.assertIsInstance(cmd, list)

    def test_parallel_job_openssl_shell_false(self):
        calls = self._run('testkey', parallel_jobs='2')
        self.assertFalse(calls[1][1].get('shell', False))

    def test_parallel_job_key_with_special_chars(self):
        dangerous_key = 'p@ss`whoami`'
        calls = self._run(dangerous_key, parallel_jobs='2')
        cmd = calls[1][0][0]
        pass_arg = cmd[cmd.index('-pass') + 1]
        self.assertEqual(pass_arg, 'pass:{}'.format(dangerous_key))

    # --- no-encryption path -----------------------------------------------

    def test_single_job_no_encryption_only_one_popen(self):
        _setup_configs()
        worker = _make_worker(encryption=False, parallel_jobs='1')
        def _fake_update(k, v, database=None, flush=False):
            worker.status[k] = v
        worker.update_status = MagicMock(side_effect=_fake_update)
        worker.fetch_roles = MagicMock(return_value='')
        worker.dump_roles_for_db = MagicMock()

        pg_backup.backups.build_backup_path.return_value = '/tmp/bkp-001'
        pg_backup.backups.build_database_backup_path.return_value = '/tmp/testdb.dump'
        pg_backup.utils.get_owner_of_db.return_value = 'postgres'

        with patch('subprocess.Popen', return_value=_mock_proc()) as mock_popen, \
             patch('builtins.open', mock_open()):
            worker.backup_single_database('testdb')

        self.assertEqual(mock_popen.call_count, 1, "No-encryption path must spawn only pg_dump")
        self.assertFalse(mock_popen.call_args[1].get('shell', False))


class TestFetchRolesPipeline(unittest.TestCase):
    """fetch_roles: chained Popen pipeline uses shell=False throughout."""

    def setUp(self):
        _setup_configs()
        _setup_psycopg2_mock()
        pg_backup.backups.build_roles_backup_path.return_value = '/tmp/roles.sql'
        pg_backup.backups.build_database_backup_path.return_value = '/tmp/testdb.dump'
        pg_backup.backups.build_backup_path.return_value = '/tmp/bkp-001'

    # --- no-encryption path -----------------------------------------------

    def _run_no_encrypt(self, parallel_jobs='1'):
        worker = _make_worker(encryption=False, parallel_jobs=parallel_jobs)
        worker.get_pg_version_from_dump = MagicMock(return_value=[14, 5])

        procs = [_mock_proc() for _ in range(4)]
        procs[3].communicate.return_value = (b'appuser\n', b'')

        with patch('pg_backup.Popen', side_effect=procs) as mock_popen, \
             patch('builtins.open', mock_open()):
            result = worker.fetch_roles('testdb')

        return mock_popen.call_args_list, result

    def test_no_encrypt_all_shell_false(self):
        calls, _ = self._run_no_encrypt()
        for c in calls:
            self.assertFalse(c[1].get('shell', False))

    def test_no_encrypt_pipeline_length(self):
        calls, _ = self._run_no_encrypt()
        self.assertEqual(len(calls), 4, "Expected pg_restore | grep | awk | uniq")

    def test_no_encrypt_pipeline_commands(self):
        calls, _ = self._run_no_encrypt()
        self.assertIn('pg_restore', calls[0][0][0][0])
        self.assertEqual(calls[1][0][0], ['grep', '-P', 'ALTER TABLE.*OWNER TO.*'])
        self.assertEqual(calls[2][0][0][0], 'awk')
        self.assertEqual(calls[3][0][0], ['uniq'])

    def test_no_encrypt_all_cmds_are_lists(self):
        calls, _ = self._run_no_encrypt()
        for c in calls:
            self.assertIsInstance(c[0][0], list)

    def test_no_encrypt_roles_returned(self):
        _, result = self._run_no_encrypt()
        self.assertEqual(result, 'appuser')

    # --- encryption path --------------------------------------------------

    def _run_encrypt(self, key='testkey'):
        worker = _make_worker(key=key, encryption=True, parallel_jobs='1')

        procs = [_mock_proc() for _ in range(5)]
        procs[4].communicate.return_value = (b'appuser\n', b'')

        with patch('pg_backup.Popen', side_effect=procs) as mock_popen, \
             patch('builtins.open', mock_open()):
            result = worker.fetch_roles('testdb')

        return mock_popen.call_args_list, result

    def test_encrypt_all_shell_false(self):
        calls, _ = self._run_encrypt()
        for c in calls:
            self.assertFalse(c[1].get('shell', False))

    def test_encrypt_pipeline_length(self):
        calls, _ = self._run_encrypt()
        self.assertEqual(len(calls), 5, "Expected openssl | pg_restore | grep | awk | uniq")

    def test_encrypt_pipeline_commands(self):
        calls, _ = self._run_encrypt()
        self.assertEqual(calls[0][0][0][0], 'openssl')
        self.assertIn('pg_restore', calls[1][0][0][0])
        self.assertEqual(calls[2][0][0], ['grep', '-P', 'ALTER TABLE.*OWNER TO.*'])
        self.assertEqual(calls[3][0][0][0], 'awk')
        self.assertEqual(calls[4][0][0], ['uniq'])

    def test_encrypt_openssl_cmd_is_list(self):
        calls, _ = self._run_encrypt()
        self.assertIsInstance(calls[0][0][0], list)

    def test_encrypt_key_as_separate_element(self):
        calls, _ = self._run_encrypt(key='mykey')
        cmd = calls[0][0][0]
        pass_arg = cmd[cmd.index('-pass') + 1]
        self.assertEqual(pass_arg, 'pass:mykey')

    def test_encrypt_key_with_backticks(self):
        dangerous_key = 'key`cat /etc/passwd`'
        calls, _ = self._run_encrypt(key=dangerous_key)
        cmd = calls[0][0][0]
        pass_arg = cmd[cmd.index('-pass') + 1]
        self.assertEqual(pass_arg, 'pass:{}'.format(dangerous_key))

    def test_encrypt_key_with_semicolon(self):
        dangerous_key = 'key;echo pwned'
        calls, _ = self._run_encrypt(key=dangerous_key)
        cmd = calls[0][0][0]
        pass_arg = cmd[cmd.index('-pass') + 1]
        self.assertEqual(pass_arg, 'pass:{}'.format(dangerous_key))


class TestDumpRolesForDbPipeline(unittest.TestCase):
    """dump_roles_for_db: chained Popen pipeline uses shell=False throughout."""

    def setUp(self):
        _setup_configs()
        pg_backup.backups.build_roles_backup_path.return_value = '/tmp/roles.sql'

    # --- no-encryption path -----------------------------------------------

    def _run_no_encrypt(self, roles='appuser|adminrole'):
        worker = _make_worker(encryption=False)
        procs = [_mock_proc(), _mock_proc(stdout_data=b'CREATE ROLE appuser;\n')]

        with patch('pg_backup.Popen', side_effect=procs) as mock_popen, \
             patch('builtins.open', mock_open()):
            worker.dump_roles_for_db(roles, 'testdb')

        return mock_popen.call_args_list

    def test_no_encrypt_all_shell_false(self):
        calls = self._run_no_encrypt()
        for c in calls:
            self.assertFalse(c[1].get('shell', False))

    def test_no_encrypt_pipeline_length(self):
        calls = self._run_no_encrypt()
        self.assertEqual(len(calls), 2, "Expected pg_dumpall | grep")

    def test_no_encrypt_pipeline_commands(self):
        calls = self._run_no_encrypt()
        self.assertIn('pg_dumpall', calls[0][0][0][0])
        self.assertEqual(calls[1][0][0][0], 'grep')

    def test_no_encrypt_roles_as_grep_pattern_element(self):
        roles = 'appuser|admin$(evil)'
        calls = self._run_no_encrypt(roles=roles)
        grep_cmd = calls[1][0][0]
        self.assertIsInstance(grep_cmd, list)
        self.assertIn(roles, grep_cmd,
                      "roles pattern must appear as a list element, not shell-interpolated")

    def test_no_role_passwords_flag_appended(self):
        pg_backup.configs.postgresql_no_role_password_flag.return_value = '--no-role-passwords'
        calls = self._run_no_encrypt()
        pg_dumpall_cmd = calls[0][0][0]
        self.assertIn('--no-role-passwords', pg_dumpall_cmd)

    def test_empty_roles_no_popen_called(self):
        worker = _make_worker(encryption=False)
        with patch('pg_backup.Popen') as mock_popen, \
             patch('builtins.open', mock_open()):
            worker.dump_roles_for_db('', 'testdb')
        mock_popen.assert_not_called()

    # --- encryption path --------------------------------------------------

    def _run_encrypt(self, key='testkey', roles='appuser'):
        worker = _make_worker(key=key, encryption=True)
        procs = [_mock_proc(), _mock_proc(), _mock_proc()]

        with patch('pg_backup.Popen', side_effect=procs) as mock_popen, \
             patch('builtins.open', mock_open()):
            worker.dump_roles_for_db(roles, 'testdb')

        return mock_popen.call_args_list

    def test_encrypt_all_shell_false(self):
        calls = self._run_encrypt()
        for c in calls:
            self.assertFalse(c[1].get('shell', False))

    def test_encrypt_pipeline_length(self):
        calls = self._run_encrypt()
        self.assertEqual(len(calls), 3, "Expected pg_dumpall | grep | openssl")

    def test_encrypt_pipeline_commands(self):
        calls = self._run_encrypt()
        self.assertIn('pg_dumpall', calls[0][0][0][0])
        self.assertEqual(calls[1][0][0][0], 'grep')
        self.assertEqual(calls[2][0][0][0], 'openssl')

    def test_encrypt_openssl_cmd_is_list(self):
        calls = self._run_encrypt()
        self.assertIsInstance(calls[2][0][0], list)

    def test_encrypt_key_as_separate_element(self):
        calls = self._run_encrypt(key='mykey')
        openssl_cmd = calls[2][0][0]
        pass_arg = openssl_cmd[openssl_cmd.index('-pass') + 1]
        self.assertEqual(pass_arg, 'pass:mykey')

    def test_encrypt_key_with_semicolon(self):
        dangerous_key = 'key;cat /etc/passwd'
        calls = self._run_encrypt(key=dangerous_key)
        openssl_cmd = calls[2][0][0]
        pass_arg = openssl_cmd[openssl_cmd.index('-pass') + 1]
        self.assertEqual(pass_arg, 'pass:{}'.format(dangerous_key))

    def test_encrypt_key_with_dollar_subshell(self):
        dangerous_key = 'key$(id)'
        calls = self._run_encrypt(key=dangerous_key)
        openssl_cmd = calls[2][0][0]
        pass_arg = openssl_cmd[openssl_cmd.index('-pass') + 1]
        self.assertEqual(pass_arg, 'pass:{}'.format(dangerous_key))


if __name__ == '__main__':
    unittest.main()
