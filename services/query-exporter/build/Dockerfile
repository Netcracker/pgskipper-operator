FROM golang:1.27-alpine3.24@sha256:fa3d596898d9f126af7c01d14ee7597bab3f39cccf392ef36b69fe77cd681f18

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