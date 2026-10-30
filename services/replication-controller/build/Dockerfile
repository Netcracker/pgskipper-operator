FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414 AS builder

ENV GOSUMDB=off

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY pkg/ pkg/
COPY cmd/ cmd/

RUN go mod tidy

# Build
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=0 go build -o ./build/_output/bin/pgskipper-replication-controller \
    -gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./cmd/pgskipper-replication-controller

FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

ENV CONTROLLER=/usr/local/bin/pgskipper-replication-controller \
    USER_UID=1001

COPY --from=builder /workspace/build/_output/bin/pgskipper-replication-controller ${CONTROLLER}

USER ${USER_UID}

CMD ["sh", "-c", "${CONTROLLER}"]
