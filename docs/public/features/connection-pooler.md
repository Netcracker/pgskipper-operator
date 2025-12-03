# Limitations

1) Custom parameters for connections are not allowed [PG bouncer settings#ignore_startup_parameters](https://www.pgbouncer.org/config.html#generic-settings)
2) For Streaming usage with enabled PgBouncer, you should use *pg-patroni-direct* service instead *pg-patroni*, to avoid connection through PgBouncer.
