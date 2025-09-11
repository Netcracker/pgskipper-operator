FROM --platform=$BUILDPLATFORM golang:1.25-alpine3.22 AS builder

ENV GOSUMDB=off

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

# Copy the go source
COPY main/ main/
COPY pkg/ pkg/

RUN go mod tidy

# Build
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o ./build/_output/bin/pgskipper-pgbackrest-sidecar \
    -gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./main/main.go

FROM alpine:3.22

ENV USER=postgres \
    HOME=/home/pg

COPY build/build-pgbackrest.sh /tmp/build-pgbackrest.sh

# Install build dependencies and runtime dependencies
RUN apk add --update --no-cache \
    # Runtime dependencies
    busybox \
    ssl_client \
    curl \
    openssh \
    # Runtime libraries for pgbackrest
    openssl \
    libxml2 \
    zlib \
    bzip2 \
    lz4 \
    zstd \
    postgresql-client \
    yaml \
    # Build dependencies for pgbackrest
    build-base \
    meson \
    ninja \
    autoconf \
    automake \
    libtool \
    pkgconfig \
    openssl-dev \
    libxml2-dev \
    zlib-dev \
    bzip2-dev \
    lz4-dev \
    zstd-dev \
    postgresql-dev \
    yaml-dev \
    wget \
    tar && \
    chmod +x /tmp/build-pgbackrest.sh && \
    sh /tmp/build-pgbackrest.sh && \
    rm /tmp/build-pgbackrest.sh && \
    # Remove build dependencies to reduce image size
    apk del \
    build-base \
    meson \
    ninja \
    autoconf \
    automake \
    libtool \
    pkgconfig \
    openssl-dev \
    postgresql-dev \
    yaml-dev \
    wget \
    tar

COPY --from=builder /workspace/build/_output/bin/pgskipper-pgbackrest-sidecar /usr/local/bin/pgskipper-pgbackrest-sidecar
COPY build/start.sh /opt/start.sh

RUN mkdir ${HOME}

RUN chgrp 0 /etc &&  \
    chmod g+w /etc && \
    chgrp 0 /etc/passwd &&  \
    chmod g+w /etc/passwd && \
    chmod 777 ${HOME} && \
    chmod 770 /var/lib/pgbackrest && \
    chmod 770 /var/log/pgbackrest && \
    chmod 770 /var/spool/pgbackrest && \
    chown postgres:0 /var/lib/pgbackrest && \
    chown postgres:0 /var/log/pgbackrest && \
    chown postgres:0 /var/spool/pgbackrest && \
    chmod 777 /opt/start.sh &&  \
    chmod +x /opt/start.sh

VOLUME /etc

CMD ["sh", "/opt/start.sh"]
USER ${USER}