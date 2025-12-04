DOCKER_FILE := build/Dockerfile

ifndef TAG_ENV
override TAG_ENV = local
endif

ifndef DOCKER_NAMES
override DOCKER_NAMES = "ghcr.io/netcracker/pgskipper-pgbackrest-sidecar:${TAG_ENV}"
endif

sandbox-build: deps compile docker-build

all: sandbox-build docker-push

local: deps compile docker-build docker-push

local-bench: deps compile-bench

deps:
	GO111MODULE=on go mod tidy
	@echo "Move helm charts"


compile:
	CGO_ENABLED=0 go build -o ./build/_output/bin/pgskipper-pgbackrest-sidecar \
                  -gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./main/main.go

docker-build:
	$(foreach docker_tag,$(DOCKER_NAMES),docker build --file="${DOCKER_FILE}" --pull -t $(docker_tag) ./;)

docker-push:
	$(foreach docker_tag,$(DOCKER_NAMES),docker push $(docker_tag);)

clean:
	rm -rf build/_output
