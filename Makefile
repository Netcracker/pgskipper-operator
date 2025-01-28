DOCKER_FILE := build/Dockerfile
TARGET := target
DISTR_NAME := postgres-operator
CAPMDGEN_DIR := target/capmdgen

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
	GOBIN=$(shell go env GOPATH)/bin
else
	GOBIN=$(shell go env GOBIN)
endif

ifndef TAG_ENV
override TAG_ENV = local
endif

ifndef DOCKER_NAMES
override DOCKER_NAMES = ghcr.io/netcracker/pgskipper-operator:${TAG_ENV}
endif

sandbox-build: deps gzip-charts move-charts deps compile docker-build

all: sandbox-build docker-push

local: fmt gzip-charts deps vet compile docker-build docker-push

deps:
	go mod tidy

gzip-charts:
	@echo "Gzipping grafana resources"
	gzip -f -c ./charts/patroni-services/monitoring/grafana-dashboard.json > ./charts/patroni-services/monitoring/grafana-dashboard.json.gz
	gzip -f -c ./charts/patroni-services/monitoring/aws-grafana-dashboard.json > ./charts/patroni-services/monitoring/aws-grafana-dashboard.json.gz
	gzip -f -c ./charts/patroni-services/monitoring/azure-grafana-dashboard.json > ./charts/patroni-services/monitoring/azure-grafana-dashboard.json.gz
	gzip -f -c ./charts/patroni-services/monitoring/cloudsql-grafana-dashboard.json > ./charts/patroni-services/monitoring/cloudsql-grafana-dashboard.json.gz
	gzip -f -c ./charts/patroni-services/monitoring/postgres-exporter-grafana-dashboard.json > ./charts/patroni-services/monitoring/postgres-exporter-grafana-dashboard.json.gz
	gzip -f -c ./charts/patroni-services/monitoring/query-exporter-grafana-dashboard.json > ./charts/patroni-services/monitoring/query-exporter-grafana-dashboard.json.gz

move-charts:
	@echo "Move helm charts"
	mkdir -p deployments/charts/patroni-services
	cp -R ./charts/patroni-services/* deployments/charts/patroni-services

fmt:
	gofmt -l -s -w .

vet:
	go vet ./...

compile:
	CGO_ENABLED=0 go build -o ./build/_output/bin/postgres-operator \
 				-gcflags all=-trimpath=${GOPATH} -asmflags all=-trimpath=${GOPATH} ./cmd/pgskipper-operator

docker-build:
	$(foreach docker_tag,$(DOCKER_NAMES),docker build --file="${DOCKER_FILE}" --pull -t $(docker_tag) ./;)


docker-push:
	$(foreach docker_tag,$(DOCKER_NAMES),docker push $(docker_tag);)

clean:
	rm -rf build/_output
	rm -rf ./$(TARGET)


# Generate code
generate: controller-gen
	$(CONTROLLER_GEN) crd:crdVersions={v1} \
					  object:headerFile="generator/boilerplate.go.txt" \
					  paths="./api/apps/v1" \
					  output:crd:artifacts:config=charts/patroni-services/crds/

	$(CONTROLLER_GEN) crd:crdVersions={v1} \
					  object:headerFile="generator/boilerplate.go.txt" \
					  paths="./api/patroni/v1" \
					  output:crd:artifacts:config=charts/patroni-core/crds/
# Find or download controller-gen
# download controller-gen if necessary
controller-gen:
ifeq (, $(shell which controller-gen))
	@{ \
	set -e ;\
	CONTROLLER_GEN_TMP_DIR=$$(mktemp -d) ;\
	cd $$CONTROLLER_GEN_TMP_DIR ;\
	go mod init tmp ;\
	go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.16.3 ;\
	rm -rf $$CONTROLLER_GEN_TMP_DIR ;\
	}
CONTROLLER_GEN=$(GOBIN)/controller-gen
else
CONTROLLER_GEN=$(shell which controller-gen)
endif
