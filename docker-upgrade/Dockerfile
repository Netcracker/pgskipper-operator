FROM ubuntu:22.04

USER root

ENV LC_ALL=en_US.UTF-8 \
    LANG=en_US.UTF-8
ARG DEBIAN_FRONTEND=noninteractive

RUN echo start
RUN mkdir -p /var/lib/pgsql/data/ && mkdir -p /var/lib/pgsql/tmp_data/

# fix for non-ha
RUN mkdir /properties && touch /properties/empty-conf.conf

RUN echo "deb [trusted=yes] http://apt.postgresql.org/pub/repos/apt jammy-pgdg main" >> /etc/apt/sources.list.d/pgdg.list
RUN ls -la /etc/apt/
RUN apt-get -y update
RUN apt-get -o DPkg::Options::="--force-confnew" -y dist-upgrade

RUN groupmod -n postgres tape
RUN adduser -uid 26 -gid 26 postgres

# Install like base image
RUN apt-get --no-install-recommends install -y gcc-12 python3.11 python3-pip python3-dev wget

RUN python3 -m pip install --no-cache-dir --upgrade wheel==0.38.0 setuptools==78.1.1

# Explicitly install patched libaom3 version
RUN apt-get --no-install-recommends install -y libaom3=3.3.0-1ubuntu0.1 || apt-get --no-install-recommends install -y libaom3

RUN apt-get -y update
RUN apt-get -o DPkg::Options::="--force-confnew" -y dist-upgrade
RUN apt-get --no-install-recommends install -y \
     postgresql-12 postgresql-server-dev-12 \
     postgresql-13 postgresql-server-dev-13 \
     postgresql-14 postgresql-server-dev-14 \
     postgresql-15 postgresql-server-dev-15 \
     postgresql-16 postgresql-server-dev-16 \
     postgresql-17 postgresql-server-dev-17 \
     postgresql-12-pg-track-settings postgresql-12-pg-wait-sampling postgresql-12-cron postgresql-12-set-user postgresql-12-pg-stat-kcache postgresql-12-pgaudit postgresql-12-pg-qualstats postgresql-12-hypopg postgresql-12-powa \
     postgresql-13-pg-track-settings postgresql-13-pg-wait-sampling postgresql-13-cron postgresql-13-set-user postgresql-13-pg-stat-kcache postgresql-13-pgaudit postgresql-13-pg-qualstats postgresql-13-hypopg postgresql-13-powa \
     postgresql-14-pg-track-settings postgresql-14-pg-wait-sampling postgresql-14-cron postgresql-14-set-user postgresql-14-postgis postgresql-14-pg-stat-kcache postgresql-14-pgaudit postgresql-14-pg-qualstats postgresql-14-hypopg postgresql-14-powa postgresql-14-pg-hint-plan postgresql-14-pgnodemx postgresql-14-decoderbufs \
     postgresql-15-pg-track-settings postgresql-15-pg-wait-sampling postgresql-15-cron postgresql-15-set-user postgresql-15-postgis postgresql-15-pg-stat-kcache postgresql-15-pgaudit postgresql-15-pg-qualstats postgresql-15-hypopg postgresql-15-powa postgresql-15-pg-hint-plan postgresql-15-pgnodemx postgresql-15-decoderbufs \
     postgresql-16-pg-track-settings postgresql-16-pg-wait-sampling postgresql-16-cron postgresql-16-set-user postgresql-16-postgis postgresql-16-pg-stat-kcache postgresql-16-pgaudit postgresql-16-pg-qualstats postgresql-16-hypopg postgresql-16-powa postgresql-16-pg-hint-plan postgresql-16-pgnodemx postgresql-16-decoderbufs \
     postgresql-17-pg-track-settings postgresql-17-pg-wait-sampling postgresql-17-cron postgresql-17-set-user postgresql-17-postgis postgresql-17-pg-stat-kcache postgresql-17-pgaudit postgresql-17-pg-qualstats postgresql-17-hypopg postgresql-17-powa postgresql-17-pg-hint-plan postgresql-17-pgnodemx postgresql-17-decoderbufs
RUN apt-get --no-install-recommends install -y libproj-dev libgdal30 libgeos3.10.2 libgeotiff5 libsfcgal1

RUN localedef -i en_US -f UTF-8 en_US.UTF-8 && \
    localedef -i es_PE -f UTF-8 es_PE.UTF-8 && \
    localedef -i es_ES -f UTF-8 es_ES.UTF-8
# Migrate .rpm to .deb
RUN apt-get install -y alien

RUN apt-get install -y protobuf-compiler

WORKDIR /tmp

COPY ./docker/start.sh /start.sh

RUN chgrp 0 /etc &&  \
    chmod g+w /etc && \
    chgrp 0 /etc/passwd &&  \
    chmod g+w /etc/passwd

RUN chgrp 0 /var/lib/pgsql/ && chmod g+w /var/lib/pgsql/ && chmod 777 /var/lib/pgsql && \
    chgrp 0 /var/run/postgresql/ && chmod g+w /var/run/postgresql/ && chmod 777 /var/run/postgresql && \
    chmod -R 777 /etc/passwd && \
    mv /usr/bin/python3.11 /usr/bin/python && ln -fs /usr/bin/python /usr/bin/python3 && \
    chmod +x /start.sh


RUN pg_config --version | grep -o "[0-9]*" | head -n 1
VOLUME /var/lib/pgsql
VOLUME /var/run/postgresql

CMD ["/bin/bash", "/start.sh"]

USER 26
