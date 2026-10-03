SHELL := /usr/bin/env bash
.DEFAULT_GOAL := build

BIN := $(CURDIR)/bin
SPECCTL := $(BIN)/specctl
SPECD := $(BIN)/specd
SPECS_KUBECONFIG ?= $(CURDIR)/.kcp-specd/admin.kubeconfig
WORKSPACE_KUBECONFIG := $(CURDIR)/.kcp-specd/specs.kubeconfig
export SPECD_KUBECONFIG ?= $(SPECS_KUBECONFIG)

export SPECD_BOLT_URL ?= bolt://127.0.0.1:7687
export SPECD_BOLT_PASSWORD_FILE ?= /tmp/hdb/token
export SPECD_BOLT_USER ?= neo4j

GO_DIRS := $(shell go list -f '{{.Dir}}' ./... 2>/dev/null)

.PHONY: build check fmt vet test test-live kcp-up kcp-down install-specs example-phase1 example-phase2 example-phase3 example-phase4 example-phase5 clean

ARCH_YAML ?= $(CURDIR)/testdata/open-architecture/arch.yaml
ARCH_REPOSITORY ?= deno-kcp

build: $(SPECCTL) $(SPECD) $(BIN)/hydradb-bins

$(SPECCTL): $(shell find cmd/specctl abc common impl -name '*.go') go.mod
	@mkdir -p $(BIN)
	go build -o $@ ./cmd/specctl

$(SPECD): $(shell find cmd/specd factory abc common impl -name '*.go') go.mod
	@mkdir -p $(BIN)
	go build -o $@ ./cmd/specd

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

# The live tests reuse the calc names in the workspace; run example-phase2 to
# put the example state back.

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

example-phase2: $(SPECCTL) kcp-up
	$(SPECCTL) apply -f examples/calc/specs.yaml
	@echo "--- index fixtures/calc with codegraph, fill status.observed, write the graph ---"
	$(SPECCTL) ingest --repo fixtures/calc
	@echo "--- observed code facts per context ---"
	$(SPECCTL) get systemcontext
	@echo "--- the calculated fingerprint and the conditions ---"
	KUBECONFIG=$(WORKSPACE_KUBECONFIG) kubectl -n default get systemcontext calc \
		-o jsonpath='{.status.observed.fingerprint}{"\n"}{range .status.conditions[*]}{.type}={.status} {end}{"\n"}'
	@echo "--- one hop of the graph around calc in HydraDB ---"
	$(SPECCTL) graph neighbors calc
	@echo "--- the same neighborhood in ArcadeDB, rebuilt from kcp and codegraph ---"
	$(SPECCTL) graph rebuild \
		--bolt-url bolt://127.0.0.1:7688 --bolt-user root --bolt-password clm-arcadedb-root --bolt-database clm
	$(SPECCTL) graph neighbors calc \
		--bolt-url bolt://127.0.0.1:7688 --bolt-user root --bolt-password clm-arcadedb-root --bolt-database clm

example-phase3: $(SPECCTL) kcp-up
	@echo "--- every node of the open architecture document becomes a SystemContext ---"
	$(SPECCTL) import-arch $(ARCH_YAML) --repository $(ARCH_REPOSITORY)
	@echo "--- one hop of the graph around sc.deno-kcp, by arch id ---"
	$(SPECCTL) graph neighbors sc.deno-kcp
	@echo "--- the same document, exported back out of kcp ---"
	$(SPECCTL) export --format arch --repository $(ARCH_REPOSITORY) -o $(CURDIR)/.kcp-specd/arch-export.yaml
	head -n 6 $(CURDIR)/.kcp-specd/arch-export.yaml
	@echo "--- import, export and the semantic diff are checked by the live test ---"
	SPECD_REQUIRE_LIVE=1 go test ./test/e2e/ -run TestPhase3ArchRoundTrip -count=1

example-phase4: $(SPECCTL) $(SPECD) kcp-up
	./scripts/example-phase4.sh

example-phase5: $(SPECCTL) $(SPECD) kcp-up
	./scripts/example-phase5.sh

clean:
	rm -f $(SPECCTL) $(SPECD) $(BIN)/hydradb-bins
