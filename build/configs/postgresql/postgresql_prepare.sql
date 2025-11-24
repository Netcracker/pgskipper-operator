/*
Function pg_patroni_service_slot_cleaner removes stuck replication slots which can damage PostgresSQL cluster.
Replication slot will be removed if they are stuck more than wal_keep_segments WAL files.
Parameters:
* ud_allowed_slot_delay - use this value instead of value of wal_keep_segments if value >=0. 0 means no delay and all slots will be removed.

Function uses dblink to connect to replicas and remove replication slots.
For more information about dblink see https://www.postgresql.org/docs/9.6/static/dblink.html.
*/
CREATE EXTENSION IF NOT EXISTS dblink;

DROP FUNCTION IF EXISTS pg_patroni_service_slot_cleaner_for_host(host text, allowed_slot_delay integer, use_old_cmp_function boolean);
CREATE FUNCTION pg_patroni_service_slot_cleaner_for_host(host text, allowed_slot_delay integer, use_old_cmp_function boolean)
  RETURNS void AS $$
DECLARE
  slots text[];
  get_slots_query text DEFAULT 'SELECT slot_name, restart_lsn, active, active_pid FROM pg_replication_slots';
  sql_query text;
  pg_password text;
  conn_string text;
  fe_slot_name RECORD;
  remove_result text;
  active_pid_result integer;
  slot_delay float;
BEGIN
  SELECT INTO pg_password command_output FROM pg_patroni_service_slot_cleaner_passwd_output LIMIT 1;

  -- perform slot cleanup for each active replica
  RAISE NOTICE 'Start check for host %', host;
  conn_string := format('hostaddr=%s port=5432 user=postgres password=' || pg_password, host);

  -- Iterate over slots and remove stuck slots
  FOR fe_slot_name IN SELECT * FROM dblink(conn_string, get_slots_query) as remote_repl_slots(slot_name text, restart_lsn pg_lsn, active boolean, active_pid integer) LOOP
    RAISE NOTICE 'Found slot %', fe_slot_name;
    IF use_old_cmp_function THEN
      SELECT INTO slot_delay pg_xlog_location_diff(pg_current_xlog_location(), fe_slot_name.restart_lsn)::bigint ;
    ELSE
      SELECT INTO slot_delay pg_wal_lsn_diff(pg_current_wal_lsn(), fe_slot_name.restart_lsn)::bigint ;
    END IF;
    RAISE NOTICE 'Slot delay % bytes', slot_delay;
    slot_delay := 1.0 * slot_delay / 1024 / 1024 ;
    RAISE NOTICE 'Slot delay % Mb, slot active: %', slot_delay, fe_slot_name.active;
    IF slot_delay > allowed_slot_delay THEN
      IF fe_slot_name.active THEN
        RAISE NOTICE 'Will terminate backend with pid % which keeps slot', fe_slot_name.active_pid;
        sql_query := 'select pg_terminate_backend(' || fe_slot_name.active_pid || ')';
        SELECT into remove_result rem_res FROM dblink(conn_string, sql_query) as remote_rem_res(rem_res text);
        RAISE NOTICE 'Result %', remove_result;
        FOR i IN 1..30 LOOP
          sql_query := 'select active, active_pid from pg_replication_slots where slot_name='|| QUOTE_LITERAL(fe_slot_name.slot_name);
          SELECT rem_active, rem_active_pid into remove_result, active_pid_result FROM dblink(conn_string, sql_query) as remote_rem_res(rem_active text, rem_active_pid integer);
          EXIT WHEN remove_result = 'f';
          RAISE NOTICE 'Wait slot {active: %, pid: % }. Will try to repeat termination.', remove_result, active_pid_result;
          sql_query := 'select pg_terminate_backend(' || active_pid_result || ')';
          SELECT into remove_result rem_res FROM dblink(conn_string, sql_query) as remote_rem_res(rem_res text);
          PERFORM pg_sleep(2);
        END LOOP;
        RAISE NOTICE 'Slot active: %', remove_result;
        IF NOT remove_result = 'f' THEN
          RAISE NOTICE 'Skip removal because slot still active';
          CONTINUE ;
        END IF;
      END IF;
      RAISE NOTICE 'Will remove slot % because delay % more than allowed value % .', fe_slot_name.slot_name, slot_delay, allowed_slot_delay;
      sql_query := 'select pg_drop_replication_slot(' || QUOTE_LITERAL(fe_slot_name.slot_name) || ')';
      SELECT into remove_result rem_res FROM dblink(conn_string, sql_query) as remote_rem_res(rem_res text);
      RAISE NOTICE 'Remove result %', remove_result;
    END IF;

    END LOOP;

    --check if slots are empty
  sql_query := 'SELECT slot_name FROM pg_replication_slots';
  slots := ARRAY(SELECT * FROM dblink(conn_string, sql_query) as remote_repl_slots(slot_name text));
  RAISE NOTICE 'Slots on host % after cleanup %', host, slots;
END; $$ LANGUAGE plpgsql;

DROP FUNCTION IF EXISTS pg_patroni_service_slot_cleaner();
DROP FUNCTION IF EXISTS pg_patroni_service_slot_cleaner(ud_allowed_slot_delay integer);
CREATE FUNCTION pg_patroni_service_slot_cleaner(ud_allowed_slot_delay integer default -1)
RETURNS void AS $$
DECLARE
	replica RECORD;
	allowed_slot_delay integer;
	use_old_cmp_function boolean;
BEGIN
	-- get password from env. shell scripts cannot be executed directly but can be executed inside COPY block
	CREATE TEMPORARY TABLE pg_patroni_service_slot_cleaner_passwd_output (tt_id serial PRIMARY KEY NOT NULL, command_output text );
	COPY pg_patroni_service_slot_cleaner_passwd_output (command_output) FROM PROGRAM 'strings /proc/1/environ | sed -n "s/^PG_ROOT_PASSWORD=\(.*\)/\1/p"';

	-- get current wal_keep_segments value and determine allowed_slot_delay
	IF ud_allowed_slot_delay < 0 THEN
    -- try legacy wal_keep_segments (PG <= 12)
		SELECT setting::integer INTO allowed_slot_delay FROM pg_settings where name='wal_keep_segments';

    IF allowed_slot_delay IS NULL THEN
      SELECT (setting::integer / 16) INTO allowed_slot_delay FROM pg_settings where name='wal_keep_size';
	ELSE
		allowed_slot_delay = ud_allowed_slot_delay;
	END IF;

	-- check if we have pg_xlog_location_diff or not (postgresql 9.6 vs postgresql 10)
	select into use_old_cmp_function exists(select * from pg_proc where proname = 'pg_xlog_location_diff');

	--todo[anin] 16Mb size per WAL file is used. Honest calculation should get value from pg_settings
	allowed_slot_delay := allowed_slot_delay * 16 ;
	RAISE NOTICE 'allowed_slot_delay: % Mb', allowed_slot_delay;

	-- perform slot cleanup for each active replica
	FOR replica IN SELECT * FROM pg_stat_replication where application_name like 'pg-%-node%' LOOP
		RAISE NOTICE 'Replica: % with addr %', replica.application_name, replica.client_addr;
    PERFORM pg_patroni_service_slot_cleaner_for_host(host(replica.client_addr), allowed_slot_delay, use_old_cmp_function);
	END LOOP;

	--check slots on master
  RAISE NOTICE 'Start master check';
  PERFORM pg_patroni_service_slot_cleaner_for_host('127.0.0.1', allowed_slot_delay, use_old_cmp_function);
END; $$ LANGUAGE plpgsql;

/**
Schedule pg_patroni_service_slot_cleaner() for execution each 10 min
 */
CREATE EXTENSION IF NOT EXISTS pg_cron;
SELECT jobid, schedule, command, cron.unschedule(jobid) FROM cron.job WHERE command like '%pg_patroni_service_slot_cleaner%';
SELECT cron.schedule('*/10 * * * *', 'select pg_patroni_service_slot_cleaner()');
SELECT * FROM cron.job;