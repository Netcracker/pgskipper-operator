FROM --platform=$BUILDPLATFORM golang:1.24.2-alpine3.20 AS builder

ENV GOSUMDB=off GOPRIVATE=github.com/Netcracker

RUN apk add --no-cache git
RUN --mount=type=secret,id=GH_ACCESS_TOKEN git config --global url."https://$(cat /run/secrets/GH_ACCESS_TOKEN)@github.com/".insteadOf "https://github.com/"

COPY . /workspace

WORKDIR /workspace

RUN go mod tidy

# Build
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o ./build/_output/bin/postgres-operator \
    -gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./cmd/pgskipper-operator

FROM alpine:3.20.3

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