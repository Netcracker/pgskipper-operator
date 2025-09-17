FROM --platform=$BUILDPLATFORM golang:1.25-alpine3.22 AS builder

WORKDIR /workspace

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . /workspace

RUN go mod tidy

# Build
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o ./build/_output/bin/postgres-operator \
    -gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./cmd/pgskipper-operator

FROM alpine:3.22

ENV OPERATOR=/usr/local/bin/postgres-operator \
    USER_UID=1001 \
    USER_NAME=postgres-operator

# install operator binary
COPY --from=builder /workspace/build/_output/bin/postgres-operator ${OPERATOR}
COPY build/bin /usr/local/bin
COPY build/configs/ /opt/operator/

RUN  /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
