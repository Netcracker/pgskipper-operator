FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder

ARG GOPROXY
ARG GOSUMDB

ENV GO111MODULE=on

# Copy the go source
COPY ./ /workspace

WORKDIR /workspace

RUN go mod tidy

# Build
ARG TARGETOS TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o ./_output/bin/metrics ./collector/main.go

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS base
COPY --from=builder /workspace/_output/bin/metrics /monitor/

ARG TARGETARCH
RUN --mount=type=cache,id=postgres-apk-alpine-3.24-${TARGETARCH},target=/var/cache/apk,sharing=locked \
    apk --cache-dir /var/cache/apk --cache-packages --cache-max-age 0 add --upgrade curl

RUN chmod +x -R /monitor/ && ln -s /lib /lib64

VOLUME /tmp

EXPOSE 8000 9273

CMD ["/monitor/metrics"]

USER 1001
