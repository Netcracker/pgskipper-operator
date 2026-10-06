FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

# Copy the go source
COPY adapter/ adapter/

RUN go mod tidy

# Build
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /build/_output/bin/postgresql-dbaas -gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./adapter

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6

ENV OPERATOR=/usr/local/bin/postgresql-dbaas \
    USER_UID=1001

# install dbaas binary
COPY --from=builder build/_output/bin/postgresql-dbaas ${OPERATOR}
COPY build/bin /usr/local/bin

USER ${USER_UID}

ENTRYPOINT ["/usr/local/bin/entrypoint"]
