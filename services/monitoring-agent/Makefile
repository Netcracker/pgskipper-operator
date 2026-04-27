DOCKER_FILE := Dockerfile

ifndef TAG_ENV
override TAG_ENV = local
endif

ifndef DOCKER_NAMES
override DOCKER_NAMES = ghcr.io/netcracker/pgskipper-docker-monitoring-agent:${TAG_ENV}
endif

sandbox-build: deps compile docker-build

all: sandbox-build docker-push

local: fmt deps compile docker-build docker-push

deps:
	go mod tidy
	GO111MODULE=on

fmt:
	gofmt -l -s -w .

compile:
	cd collector && CGO_ENABLED=0 go build -o ../build/_output/bin/pgskipper-monitoring-agent \
		-gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} .

docker-build:
	$(foreach docker_tag,$(DOCKER_NAMES),docker build --file="${DOCKER_FILE}" --pull -t $(docker_tag) ./;)

docker-push:
	$(foreach docker_tag,$(DOCKER_NAMES),docker push $(docker_tag);)

clean:
	rm -rf build/_output

test:
	go test -v ./...
