DOCKER_FILE := build/Dockerfile

ifndef TAG_ENV
override TAG_ENV = local
endif

ifndef DOCKER_NAMES
override DOCKER_NAMES = "ghcr.io/netcracker/qubership-query-exporter:${TAG_ENV}"
endif

sandbox-build: deps docker-build

all: sandbox-build docker-push

local: deps docker-build docker-push

deps:
	go mod tidy
	GO111MODULE=on

fmt:
	gofmt -l -s -w .


docker-build:
	$(foreach docker_tag,$(DOCKER_NAMES),docker build --file="${DOCKER_FILE}" --pull -t $(docker_tag) ./;)

docker-push:
	$(foreach docker_tag,$(DOCKER_NAMES),docker push $(docker_tag);)

clean:
	rm -rf build/_output

test:
	go test -v ./...
