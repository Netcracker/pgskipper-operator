SHELL := /bin/bash
ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
GO_MODULES := $(sort $(patsubst %/go.mod,%,$(shell find $(ROOT)/operator $(ROOT)/services -name go.mod -not -path '*/vendor/*')))
GOLANGCI_CONFIG := $(ROOT)/.github/linters/.golangci.yml
FLAKE8_CONFIG := $(ROOT)/.github/linters/.flake8

.PHONY: fmt vet lint test verify build modules

modules:
	@printf '%s\n' $(GO_MODULES)


fmt:
	@echo "==> Checking GO formatting"
	@files=$$(find "$(ROOT)/operator" "$(ROOT)/services" \
		-type f -name '*.go' \
		-not -path '*/vendor/*' \
		-exec gofmt -l -s {} +); \
	if [ -n "$$files" ]; then \
		echo "Go files requiring formatting:"; \
		printf '%s\n' "$$files"; \
		exit 1; \
	fi
	@echo "Formatting OK"


vet:
	@set -e; \
	for module in $(GO_MODULES); do \
		echo "==> Vet: $$module"; \
		(cd "$$module" && go vet ./...); \
	done

lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint is not installed"; exit 1; }
	@command -v staticcheck >/dev/null || { echo "staticcheck is not installed"; exit 1; }
	@python3 -m flake8 --version >/dev/null || { echo "Python flake8 is not installed"; exit 1; }
	@set -e; \
	for module in $(GO_MODULES); do \
		echo "==> golangci-lint: $$module"; \
		(cd "$$module" && golangci-lint run --config "$(GOLANGCI_CONFIG)" ./...); \
	done
	@echo "==> Python flake8"
	@cd "$(ROOT)" && python3 -m flake8 --config "$(FLAKE8_CONFIG)" \
		services/backup-daemon \
		services/patroni \
		tests


test:
	@set -e; \
	for module in $(GO_MODULES); do \
		echo "==> Testing: $$module"; \
		(cd "$$module" && go test ./...); \
	done


build:
	@set -e; \
	for module in $(GO_MODULES); do \
		echo "==> Building: $$module"; \
		(cd "$$module" && go build ./...); \
	done


verify: fmt vet lint test
	@echo "==> All verification checks passed!"