import os
import psycopg2

from scripts.collector_utils import get_host, get_port


def main():
    monitoring_pw = os.environ.get("MONITORING_PASSWORD", "monitoring_role")
    monitoring_role = os.environ.get("MONITORING_USER", "monitoring_role")

    conn_properties = dict(
        host=get_host(), port=get_port(), user=monitoring_role,
        password=monitoring_pw, database='postgres'
    )
    result_data = {"status": "UP"}

    nodes = {}
    pool_nodes_info = []

    try:
        pool_nodes_info = get_pool_nodes(get_connection_properties(monitoring_role, monitoring_pw))
    except Exception as e:

        return

    for node in pool_nodes_info:
        if node[3] != 'up':
            result_data.update({"status": "PROBLEM"})
        nodes.update({
            node[1]: {
                "status": node[3],
                "select_count": node[6]
            }})

    result_data.update({"pg-pool": nodes})

    print(result_data)



def get_connection_properties(user, password):
    conn_string = f"host='{get_host()}' dbname='postgres' " \
                  f"user='{user}' " \
                  f"password='{password}' " \
                  f"port='{get_port()}' "
    return conn_string



def get_pool_nodes(conn_properties):
    with psycopg2.connect(**conn_properties) as conn:
        with conn.cursor() as cur:
            cur.execute('show pool_nodes;')
            return cur.fetchall()


if __name__ == '__main__':
    main()
