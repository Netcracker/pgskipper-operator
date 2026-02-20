FROM --platform=$BUILDPLATFORM golang:1.26-alpine3.23 AS builder

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

FROM alpine:3.23

ENV CONTROLLER=/usr/local/bin/pgskipper-replication-controller \
    USER_UID=1001

COPY --from=builder /workspace/build/_output/bin/pgskipper-replication-controller ${CONTROLLER}

USER ${USER_UID}

CMD ["sh", "-c", "${CONTROLLER}"]
