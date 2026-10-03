SHELL := /usr/bin/env bash
.DEFAULT_GOAL := build

BIN := $(CURDIR)/bin
SPECCTL := $(BIN)/specctl
SPECS_KUBECONFIG ?= $(CURDIR)/.kcp-specd/admin.kubeconfig
WORKSPACE_KUBECONFIG := $(CURDIR)/.kcp-specd/specs.kubeconfig
export SPECD_KUBECONFIG ?= $(SPECS_KUBECONFIG)

GO_DIRS := $(shell go list -f '{{.Dir}}' ./... 2>/dev/null)

.PHONY: build check fmt vet test test-live kcp-up kcp-down install-specs example-phase1 clean

build: $(SPECCTL) $(BIN)/hydradb-bins

$(SPECCTL): $(shell find cmd/specctl abc common impl -name '*.go') go.mod
	@mkdir -p $(BIN)
	go build -o $@ ./cmd/specctl

$(BIN)/hydradb-bins: $(shell find cmd/hydradb-bins -name '*.go') go.mod
	@mkdir -p $(BIN)
	go build -o $@ ./cmd/hydradb-bins

check: fmt vet

fmt:
	@unformatted="$$(gofmt -l $(GO_DIRS))"; \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

vet:
	go vet ./...

test:
	go test -short ./...

test-live:
	SPECD_REQUIRE_LIVE=1 go test ./... -count=1

kcp-up:
	./deploy/start-kcp.sh

kcp-down:
	./deploy/stop-kcp.sh

install-specs:
	./deploy/install-specs.sh

example-phase1: $(SPECCTL) kcp-up
	$(SPECCTL) apply -f examples/calc/specs.yaml
	$(SPECCTL) get systemcontext
	@echo "--- the same objects through kubectl, on the workspace kubeconfig ---"
	KUBECONFIG=$(WORKSPACE_KUBECONFIG) kubectl get systemcontexts
	@echo "--- one SystemContext as YAML ---"
	$(SPECCTL) get systemcontext calc -o yaml

clean:
	rm -f $(SPECCTL) $(BIN)/hydradb-bins
