DOCKER_FILE := Dockerfile

ifndef TAG_ENV
override TAG_ENV = local
endif

ifndef DOCKER_NAMES
override DOCKER_NAMES = ghcr.io/netcracker/pgskipper-docker-backup-daemon:${TAG_ENV}
endif

sandbox-build: docker-build

all: sandbox-build docker-push

local: docker-build docker-push

docker-build:
	$(foreach docker_tag,$(DOCKER_NAMES),docker build --file="${DOCKER_FILE}" --pull -t $(docker_tag) ./;)

docker-push:
	$(foreach docker_tag,$(DOCKER_NAMES),docker push $(docker_tag);)

clean:
	rm -rf go/build/_output
	find . -type d -name "__pycache__" -exec rm -rf {} + 2>/dev/null || true
	find . -type f -name "*.pyc" -delete 2>/dev/null || true

# Go module in go/ subdirectory
go-deps:
	cd go && go mod tidy && GO111MODULE=on

go-fmt:
	cd go && gofmt -l -s -w .

go-compile:
	cd go && CGO_ENABLED=0 go build -o ./build/_output/bin/pgskipper-backup-daemon-azure \
		-gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./cmd/pgskipper-backup-daemon-azure

go-test:
	cd go && go test -v ./...
