FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.24@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523 AS builder

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

FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a

ENV OPERATOR=/usr/local/bin/postgresql-dbaas \
    USER_UID=1001

# install dbaas binary
COPY --from=builder build/_output/bin/postgresql-dbaas ${OPERATOR}
COPY build/bin /usr/local/bin

USER ${USER_UID}

ENTRYPOINT ["/usr/local/bin/entrypoint"]
