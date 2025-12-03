DOCKER_FILE := build/Dockerfile
NAMESPACE := 

ifndef DOCKER_NAMES
override DOCKER_NAMES = "${IMAGE_NAME}"
endif

sandbox-build: deps compile docker-build

all: sandbox-build docker-push

local: fmt deps compile docker-build docker-push

deps:
	go mod tidy
	GO111MODULE=on

update:
	go get -u ./...

fmt:
	gofmt -l -s -w .

compile:
	CGO_ENABLED=0 go build -o ./build/_output/bin/pgskipper-replication-controller \
				-gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./cmd/pgskipper-replication-controller


docker-build:
	$(foreach docker_tag,$(DOCKER_NAMES),docker build --file="${DOCKER_FILE}" --pull -t $(docker_tag) ./;)

docker-push:
	$(foreach docker_tag,$(DOCKER_NAMES),docker push $(docker_tag);)

clean:
	rm -rf build/_output

test:
	go test -v ./...

replace-image: local
	$(foreach docker_tag,$(DOCKER_NAMES),kubectl patch deployment pgskipper-replication-controller -n $(NAMESPACE) --type "json" -p '[{"op":"replace","path":"/spec/template/spec/containers/0/image","value":'$(docker_tag)'},{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"Always"}, {"op":"replace","path":"/spec/replicas","value":0}]';)
	$(foreach docker_tag,$(DOCKER_NAMES),kubectl patch deployment pgskipper-replication-controller -n $(NAMESPACE) --type "json" -p '[{"op":"replace","path":"/spec/replicas","value":1}]';)