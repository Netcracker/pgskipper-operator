FROM --platform=$BUILDPLATFORM golang:1.25-alpine3.23 AS builder

ENV GO111MODULE=on

# Copy the go source
COPY go /workspace

WORKDIR /workspace

RUN go mod tidy

# Build
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o ./_output/bin/azure_restore ./cmd/main.go

FROM ubuntu:22.04

ENV PGPASSWORD=password PG_CLUSTER_NAME=common STORAGE_ROOT=/backup-storage EXTERNAL_STORAGE_ROOT=/external \
    LC_ALL=en_US.UTF-8 \
    LANG=en_US.UTF-8

COPY docker/pip.conf /root/.pip/pip.conf

RUN echo "deb [trusted=yes] http://apt.postgresql.org/pub/repos/apt jammy-pgdg main" >> /etc/apt/sources.list.d/pgdg.list
RUN cat /etc/apt/sources.list
RUN ls -la /etc/apt/
#TEST
RUN failed apt-get -y update
RUN apt-get -o DPkg::Options::="--force-confnew" -y dist-upgrade
RUN apt-get update && \
    apt-get install -y --allow-downgrades gcc-12 cpp-12 gcc-12-base libgcc-12-dev libstdc++6 libgcc-s1 libnsl2
RUN apt-get --no-install-recommends install -y python3.11 python3.11-dev python3-pip libpq-dev cython3 \
    && update-alternatives --install /usr/bin/python3 python3 /usr/bin/python3.11 1

RUN ls -ls /usr/bin/
ARG DEBIAN_FRONTEND=noninteractive
RUN apt-get --no-install-recommends install -y comerr-dev \
                       unzip \
                       build-essential \
                       manpages-dev \
                       libkrb5-dev \
                       libsasl2-dev libldap2-dev libssl-dev \
                       postgresql-13 postgresql-14 postgresql-15 postgresql-16 postgresql-17 postgresql-18 \
                       jq \
                       openssl curl \
                       vim \
                       locales
RUN python3 -m pip install -U setuptools==78.1.1 wheel==0.38.0

COPY docker/requirements.txt /root/requirements.txt
RUN python3 -m pip install --no-cache-dir -r /root/requirements.txt \
      && python3 -m pip install --upgrade pip \
      && python3 -m pip install grpcio \
      && python3 -m pip install opentelemetry-distro opentelemetry-exporter-otlp opentelemetry-api opentelemetry-sdk opentelemetry-instrumentation-flask \
      && opentelemetry-bootstrap -a install \
      && pip3 uninstall -y pip \
      && apt-get remove -y --purge gcc-12 \
      && apt-get remove -y --purge python3-dev \
      && apt-get remove -y --purge libpq-dev \
      && apt-get remove -y --purge cython3 \
      && locale-gen en_US.UTF-8 \
      && apt-get clean

RUN ln -s /usr/bin/python3 /usr/bin/python

COPY --from=builder /workspace/_output/bin/azure_restore /opt/backup/
COPY docker/postgres/ docker/health.sh /opt/backup/
COPY docker/granular /opt/backup/granular
COPY docker/postgres/encryption.py /opt/backup/granular/encryption.py
COPY docker/external_scripts/azure_restore.sh /opt/backup/
COPY maintenance /maintenance

RUN mkdir -p /backup-storage/ && \
    mkdir -p /external/ && \
    chmod -R +x /opt/backup/ && \
    chmod -R 777 /opt/backup/ && \
    chmod -R 777 /backup-storage/ && \
    chmod -R 777 /external/ && \
    chmod -R g+w /maintenance/recovery/ && \
    chmod +x /maintenance/recovery/*.sh && \
    chgrp -R 0 /backup-storage/ && \
    chgrp -R 0 /external/ && \
    chmod g+w /etc && \
    chgrp 0 /etc/passwd &&  \
    chmod g+w /etc/passwd


#VOLUME /backup-storage
#VOLUME /external
# Volumes are defined to support read-only root file system
VOLUME /etc
VOLUME /opt/backup
VOLUME /tmp

EXPOSE 8080 8081 8082 9000

CMD ["bash", "/opt/backup/start_backup_daemon.sh"]