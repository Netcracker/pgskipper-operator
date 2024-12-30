FROM --platform=$BUILDPLATFORM golang:1.22.5-alpine3.20 AS builder

ENV GOSUMDB=off GOPRIVATE=github.com/Netcracker

WORKDIR /workspace

RUN apk add --no-cache git

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum

RUN --mount=type=secret,id=GH_ACCESS_TOKEN git config --global url."https://$(cat /run/secrets/GH_ACCESS_TOKEN)@github.com/".insteadOf "https://github.com/"

RUN go mod download

# Copy the go source
COPY adapter/ adapter/

RUN go mod tidy

# Build
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /build/_output/bin/postgresql-dbaas -gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./adapter

FROM alpine:3.20

ENV OPERATOR=/usr/local/bin/postgresql-dbaas \
    USER_UID=1001

# install dbaas binary
COPY --from=builder build/_output/bin/postgresql-dbaas ${OPERATOR}
COPY build/bin /usr/local/bin

USER ${USER_UID}

ENTRYPOINT ["/usr/local/bin/entrypoint"]
