DOCKER_FILE := Dockerfile

ifndef TAG_ENV
override TAG_ENV = local
endif

ifndef PG_VERSION
override PG_VERSION = 15
endif

ifndef DOCKER_NAMES
override DOCKER_NAMES = ghcr.io/netcracker/pgskipper-docker-patroni:${TAG_ENV}
endif

sandbox-build: docker-build

all: sandbox-build docker-push

local: docker-build docker-push

docker-build:
	$(foreach docker_tag,$(DOCKER_NAMES),docker build --file="${DOCKER_FILE}" --pull --build-arg PG_VERSION=$(PG_VERSION) -t $(docker_tag) ./;)

docker-push:
	$(foreach docker_tag,$(DOCKER_NAMES),docker push $(docker_tag);)

clean:
	rm -rf build/_output
	find . -type f -name "*.pyc" -delete 2>/dev/null || true
	find . -type d -name "__pycache__" -exec rm -rf {} + 2>/dev/null || true
