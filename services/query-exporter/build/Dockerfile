FROM golang:1.27-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280

ARG GOPROXY

ENV EXPORTER_FILE=/usr/local/bin/query-exporter \
    USER_UID=1001

COPY build/alpine-repositories /etc/apk/repositories

ARG TARGETARCH
ARG ALPINE_APK_MIRROR
RUN if [ -n "$ALPINE_APK_MIRROR" ]; then \
        sed -Ei "s#^https?://[^/]+/alpine/#${ALPINE_APK_MIRROR}/#" /etc/apk/repositories && \
        grep -F "$ALPINE_APK_MIRROR/" /etc/apk/repositories; \
    fi
RUN --mount=type=cache,id=postgres-apk-alpine-3.24-${TARGETARCH},target=/var/cache/apk,sharing=locked \
    apk --cache-dir /var/cache/apk --cache-packages --cache-max-age 0 add --upgrade gcc
RUN --mount=type=cache,id=postgres-apk-alpine-3.24-${TARGETARCH},target=/var/cache/apk,sharing=locked \
    apk --cache-dir /var/cache/apk --cache-packages --cache-max-age 0 add --upgrade build-base

COPY . /workdir
WORKDIR /workdir

RUN go mod tidy

RUN env GOOS=$TARGETOS GOARCH=$TARGETARCH CGO_ENABLED=1 go build -o /build/_output/query-exporter \
 				-gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH}

RUN cp /build/_output/query-exporter ${EXPORTER_FILE}


COPY build/bin /usr/local/bin


USER ${USER_UID}

ENTRYPOINT ["/usr/local/bin/entrypoint"]
