import os
import logging

SA_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

cluster_name = os.environ.get("PGCLUSTER", "common")
pg_port = os.environ.get("PGPORT", 5432)
monitoring_role = '"' + os.environ.get("MONITORING_USER", "monitoring_role") + '"'
monitoring_pw = os.environ.get("MONITORING_PASSWORD", "monitoring_password")

queries = [
    "DROP TABLE if exists monitor_test",
    f"CREATE ROLE {monitoring_role} with login password '{monitoring_pw}'",
    f"ALTER ROLE {monitoring_role} with login password '{monitoring_pw}'",
    f"GRANT pg_monitor to {monitoring_role}",
    f"GRANT pg_read_all_data to {monitoring_role}",
    "CREATE EXTENSION if not exists pg_stat_statements",
    f"GRANT CREATE ON SCHEMA public TO {monitoring_role}",
    "DROP SEQUENCE if exists monitor_test_seq"
]

secured_views = ['pg_stat_replication', 'pg_stat_statements', 'pg_database', 'pg_stat_activity']

func_template = """
    DROP FUNCTION IF EXISTS func_{0}();
"""

logging.basicConfig(
    filename='/proc/1/fd/1',
    filemode='w',
    level=logging.INFO,
    format='[%(asctime)s,%(msecs)03d][%(levelname)-5s][category=%(name)s]'
           '[pid=%(process)d] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S'
)

logger = logging.getLogger()


def main():
    logger.info("Will run preparation scripts")
    pg_user, pg_password = get_postgres_credentials()
    import psycopg2
    conn = psycopg2.connect(get_connection_properties(pg_user, pg_password))
    conn.autocommit = True
    with conn.cursor() as cursor:
        def execute_silently(query_):
            logger.debug("Executing next query: {}".format(query_))
            try:
                cursor.execute(query_)
            except psycopg2.Error:
                logger.exception(
                    "Exception happened during execution of the query")

        list(map(execute_silently, queries))
        list(map(execute_silently, [func_template.format(view) for view in secured_views]))
    conn.close()
    logger.info("Run of preparation scripts has been completed")


def get_postgres_credentials():
    from kubernetes import config
    from kubernetes.client.apis import core_v1_api
    from kubernetes.client.rest import ApiException
    config.load_incluster_config()
    api = core_v1_api.CoreV1Api()
    namespace = open(SA_PATH).read()
    try:
        user = os.getenv("PG_ROOT_USER")
        password = os.getenv("PG_ROOT_PASSWORD")
        if user or password:
            return user, password
        secret_name = os.environ.get("POSTGRESQL_CREDENTIALS", f"{cluster_name}-pg-root-credentials")
        api_response = api.read_namespaced_secret(secret_name, namespace)
        import base64
        data = api_response.data
        password = base64.b64decode(data.get("password")).decode('utf-8')
        user_data = data.get("user")
        if not user_data:
            user_data = data.get("username")
        user = base64.b64decode(user_data).decode('utf-8')
        return user, password
    except ApiException as exc:
        logger.error(exc)
        raise exc


def get_connection_properties(user, password):
    conn_string = f"host='{get_host()}' dbname='postgres' " \
                  f"user='{user}' " \
                  f"password='{password}' " \
                  f"port='{get_port()}' "
    return conn_string


def get_host():
    cluster_name = os.getenv('PGCLUSTER', 'patroni')
    namespace = os.getenv('NAMESPACE', 'postgres-service')
    host = os.getenv('POSTGRES_HOST', f'pg-{cluster_name}.{namespace}')
    return host


def get_port():
    return int(os.getenv('POSTGRES_PORT', 5432))


if __name__ == '__main__':
    main()
