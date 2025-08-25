# Copyright 2024-2025 NetCracker Technology Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

FROM --platform=$BUILDPLATFORM golang:1.23-alpine3.20 AS builder

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