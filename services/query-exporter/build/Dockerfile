FROM golang:1.27-alpine3.24@sha256:eb61c3873c97416fff0116553fff65d67a3549f72e11d9758849a689cdd8b523

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