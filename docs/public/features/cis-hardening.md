CIS is a forward-thinking, non-profit entity that harnesses the power of a global IT community to safeguard private and public organizations against cyber threats.

CIS benchmarks are best practices for the secure configuration of a target system.

For more information about PostgreSQL CIS benchmarks and other benchmarks, refer to [https://www.cisecurity.org/cis-benchmarks/](https://www.cisecurity.org/cis-benchmarks/). 

# Overview

Hardening is a process of limiting potential weaknesses that make systems vulnerable to cyber attacks.

Compliance with CIS benchmark should be achieved to perform hardening.

Apart from the database software itself, data security involves many areas of the operating environment where PostgreSQL is used. 

The CIS PostgreSQL benchmark provides recommendations in the following areas:

* Installation and patches
* Directory and file permissions
* Logging monitoring and auditing
* User authentication, access controls, and authorization
* Connection and replication
* PostgreSQL settings and special configuration considerations

## Achieving CIS Hardening 

Most of the recommendations are followed naturally, but some of them can be achieved through configuration parameters.

## Configuration of Benchmark

While running the CIS benchmark, it is possible to configure Policy Values for some of the PostgreSQL parameters.

The following table describes the custom values for such parameters:

| Policy                 | Default Policy Value |  Recommended Policy Value  | Description |
|------------------------|---------------------|-----|----------|
|3.1.4  Ensure the log file destination directory is set correctly. | `log` | `/proc/1/fd` | In case of deployment to Kubernetes or OpenShift, we should forward all the logs to `1` process. So the `fluentd` agent should be able to forward logs to Graylog. |
|3.1.5  Ensure the filename pattern for log files is set correctly. | `postgresql-%a.log` | `1` | In case of deployment to Kubernetes or OpenShift, we should forward all the logs to `stdout` of `1` process. So the `fluentd` agent should be able to forward logs to Graylog. |
|3.1.8  Ensure the maximum log file lifetime is set correctly. | `1d` | `0` | In case of deployment to Kubernetes or OpenShift, we should forward all the logs to Graylog. The logs' rotation policy is configured system-wide for Kubernetes or OpenShift and for Graylog. |
|3.1.18 Ensure 'log_connections' is enabled. | `on` | `off` | By default, we are not enabling this parameter, but it is possible to enable it. For more information, see the [Configuring PostgreSQL Parameters](#configuring-postgresql-parameters) section. |
|3.1.18 Ensure 'log_disconnections' is enabled. | `on` | `off` | By default, we are not enabling this parameter, but it is possible to enable it. For more information, see the [Configuring PostgreSQL Parameters](#configuring-postgresql-parameters) section. |
|3.1.22 Ensure 'log_line_prefix' is set correctly. | `%m` | `[%m][source=postgresql]` | In case of deployment to Kubernetes or OpenShift, we should forward all the logs to Graylog. Such `log_line_prefix` allows to filter all the PostgreSQL logs through the `source=postgresql` prefix. |
|3.1.23 Ensure 'log_hostname' is set correctly | `off` | `on` | Enabling the log_hostname setting causes the hostname of the connecting host to be logged in addition to the host's IP address for connection log messages. |
|3.1.24 Ensure 'log_timezone' is set correctly. | `us/eastern` | `UTC` | We are setting `UTC` timezone for our PostgreSQL deployment. But it is possible to change this value. |
|3.2    Ensure the PostgreSQL Audit Extension (pgAudit) is enabled - pgaudit installed. | `pgaudit` | `pg_stat_statements, pg_hint_plan, pg_cron, pgaudit, set_user` | By default, we are not enabling this parameter, but it is possible to enable it. For more information, see the [Configuring PostgreSQL Parameters](#configuring-postgresql-parameters) section. |
|4.7    Ensure the set_user extension is installed. | `set_user` | `pg_stat_statements, pg_hint_plan, pg_cron, pgaudit, set_user` | By default, we are not enabling this parameter, but it is possible to enable it. For more information, see the [Configuring PostgreSQL Parameters](#configuring-postgresql-parameters) section. |
|6.8    Ensure SSL is enabled and configured correctly. | `on` | `off` | PostgreSQL is not exposed outside of OpenShift or Kubernetes, so enabling of SSL is needless. If necessary, it is better to configure SSL on the PaaS Level (OpenShift or Kubernetes). |
|6.9    Ensure that pgcrypto extension is installed and configured correctly. | `"pgcrypto", regex:".*", regex:".*", regex:".*"` | `"pgcrypto", "1.3", NULL, "cryptographic functions"` | Pgcrypto extension is installed in PostgreSQL Docker images by default, but the extension should be activated on Logical Database level by applications. |

### Configuring PostgreSQL Parameters

To configure PostgreSQL parameters:

* Add `set_user` extension in the `shared_preload_libraries` parameter.
* Enable `log_connections` and `log_disconnections` options through the `postgre_confs` parameter. 

```yaml
patroni:
  postgreSQLParams:
    - "log_connections=on"
    - "log_hostname=on"
    - "log_disconnections=on"
    - "shared_preload_libraries=pg_stat_statements, pg_hint_plan, pg_cron, pgaudit, set_user"
    - "log_line_prefix=[%m][source=postgresql]"
    - "pgaudit.log=ddl,role,write"
    - "pgaudit.log_relation=on"
```
