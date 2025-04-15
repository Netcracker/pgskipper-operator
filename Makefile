DOCKER_FILE := build/Dockerfile

NAMESPACE := 

ifndef TAG_ENV
override TAG_ENV = local
endif

ifndef DOCKER_NAMES
override DOCKER_NAMES = "ghcr.io/netcracker/pgskipper-dbaas-adapter:${TAG_ENV}"
endif

sandbox-build: deps docker-build

all: sandbox-build docker-push

local: fmt deps docker-build

deps:
	go mod tidy
	GO111MODULE=on

fmt:
	gofmt -l -s -w .

compile:
	CGO_ENABLED=0 go build -o ./build/_output/bin/postgresql-dbaas \
				-gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./adapter


docker-build:
	$(foreach docker_tag,$(DOCKER_NAMES),docker build --file="${DOCKER_FILE}" --pull -t $(docker_tag) ./;)

docker-push:
	$(foreach docker_tag,$(DOCKER_NAMES),docker push $(docker_tag);)

clean:
	rm -rf build/_output

test:
	git config --global url."https://${GH_ACCESS_TOKEN}@github.com/".insteadOf "https://github.com/"
	go test -v ./...

replace-image: local
	$(foreach docker_tag,$(DOCKER_NAMES),kubectl patch deployment dbaas-postgres-adapter -n $(NAMESPACE) --type "json" -p '[{"op":"replace","path":"/spec/template/spec/containers/0/image","value":'$(docker_tag)'},{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"Always"}, {"op":"replace","path":"/spec/replicas","value":0}]';)
	$(foreach docker_tag,$(DOCKER_NAMES),kubectl patch deployment dbaas-postgres-adapter -n $(NAMESPACE) --type "json" -p '[{"op":"replace","path":"/spec/replicas","value":1}]';)