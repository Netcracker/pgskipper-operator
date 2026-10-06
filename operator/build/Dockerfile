FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder

WORKDIR /workspace

COPY go.mod go.sum ./

RUN --mount=type=cache,id=gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=gobuildcache,target=/root/.cache/go-build \
    go mod download

COPY . .

# Build
ARG TARGETOS
ARG TARGETARCH

RUN --mount=type=cache,id=gomodcache,target=/go/pkg/mod \
    --mount=type=cache,id=gobuildcache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o ./build/_output/bin/postgres-operator \
    -gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./cmd/pgskipper-operator

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

ENV OPERATOR=/usr/local/bin/postgres-operator \
    USER_UID=1001

# Upgrade zlib to fix CVE-2026-22184 (CRITICAL) and CVE-2026-27171 (MEDIUM)
# Temporal fix, will be removed when alpine will be fixed
RUN apk upgrade --no-cache zlib

# install operator binary
COPY --from=builder /workspace/build/_output/bin/postgres-operator ${OPERATOR}
COPY build/bin /usr/local/bin
COPY build/configs/ /opt/operator/

RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/postgres-operator"]

USER ${USER_UID}
