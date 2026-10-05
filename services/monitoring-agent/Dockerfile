FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.23 AS builder

ENV GO111MODULE=on

# Copy the go source
COPY ./ /workspace

WORKDIR /workspace

RUN go mod tidy

# Build
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o ./_output/bin/metrics ./collector/main.go

FROM alpine:3.24@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS base
COPY --from=builder /workspace/_output/bin/metrics /monitor/

RUN apk add --upgrade --no-cache curl

RUN chmod +x -R /monitor/ && ln -s /lib /lib64

VOLUME /tmp

EXPOSE 8000 9273

CMD ["/monitor/metrics"]

USER 1001
