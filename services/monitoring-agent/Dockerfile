FROM --platform=$BUILDPLATFORM golang:1.27-alpine3.24@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS builder

ENV GO111MODULE=on

# Copy the go source
COPY ./ /workspace

WORKDIR /workspace

RUN go mod tidy

# Build
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o ./_output/bin/metrics ./collector/main.go

FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a AS base
COPY --from=builder /workspace/_output/bin/metrics /monitor/

RUN apk add --upgrade --no-cache curl

RUN chmod +x -R /monitor/ && ln -s /lib /lib64

VOLUME /tmp

EXPOSE 8000 9273

CMD ["/monitor/metrics"]

USER 1001
