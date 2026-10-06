FROM golang:1.27-alpine3.24@sha256:8a5910f31396cd4d89662f56c68b3ae31d374308270a1c3bd96672ee5ed43414

ENV EXPORTER_FILE=/usr/local/bin/query-exporter \
    USER_UID=1001

COPY build/alpine-repositories /etc/apk/repositories

RUN apk add --upgrade --no-cache gcc 
RUN apk add --upgrade --no-cache build-base

COPY . /workdir
WORKDIR /workdir

RUN go mod tidy

RUN env GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=1 go build -o /build/_output/query-exporter \
 				-gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH}

RUN cp /build/_output/query-exporter ${EXPORTER_FILE}


COPY build/bin /usr/local/bin


USER ${USER_UID}

ENTRYPOINT ["/usr/local/bin/entrypoint"]