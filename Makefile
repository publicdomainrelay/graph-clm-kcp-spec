SHELL := /usr/bin/env bash
.DEFAULT_GOAL := build

BIN := $(CURDIR)/bin
SPECCTL := $(BIN)/specctl
SPECD := $(BIN)/specd
SPECS_KUBECONFIG ?= $(CURDIR)/.kcp-specd/admin.kubeconfig
WORKSPACE_KUBECONFIG := $(CURDIR)/.kcp-specd/specs.kubeconfig
export SPECD_KUBECONFIG ?= $(SPECS_KUBECONFIG)

# The commit the binaries were built from, stamped in so an example run can
# refuse a binary that does not match HEAD.
BUILD_COMMIT := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
BUILD_DIRTY := $(if $(shell git status --porcelain 2>/dev/null),dirty,)
LDFLAGS := -X main.buildCommit=$(BUILD_COMMIT) -X main.buildDirty=$(BUILD_DIRTY)

# The graph backend every target defaults to. ArcadeDB is the default; the Go
# flag and environment defaults follow this variable, so
# `make test-live SPECD_BOLT_BACKEND=hydradb` switches every command to HydraDB
# on bolt://127.0.0.1:7687 with the token in /tmp/hdb/token.
export SPECD_BOLT_BACKEND ?= arcadedb

GO_DIRS := $(shell go list -f '{{.Dir}}' ./... 2>/dev/null)

.PHONY: build check fmt vet generate-schemas test test-live test-live-model kcp-up kcp-down install-specs install-specs-provider example-phase1 example-phase2 example-phase3 example-phase4 example-phase5 example-phase6 example-phase6-failing example-phase7 example-phase8 example-phase9 example-phase13 demo demo-phases clean FORCE

FORCE:

ARCH_YAML ?= $(CURDIR)/testdata/open-architecture/arch.yaml
ARCH_REPOSITORY ?= deno-kcp

build: $(SPECCTL) $(SPECD) $(BIN)/hydradb-bins

# A commit change has to rebuild the stamped binaries even when no source file
# moved, so the example script's stale-build check never points at a no-op
# make build.
$(BIN)/.commit: FORCE
	@mkdir -p $(BIN)
	@printf '%s %s\n' '$(BUILD_COMMIT)' '$(BUILD_DIRTY)' > $@.tmp
	@cmp -s $@.tmp $@ || mv $@.tmp $@
	@rm -f $@.tmp

POLICY_EMBEDS := $(shell find impl/policyeval/lib impl/effects/packs policies/packs -type f)

$(SPECCTL): $(shell find cmd/specctl abc common impl -name '*.go') $(POLICY_EMBEDS) go.mod $(BIN)/.commit
	@mkdir -p $(BIN)
	go build -ldflags '$(LDFLAGS)' -o $@ ./cmd/specctl

$(SPECD): $(shell find cmd/specd factory abc common impl -name '*.go') go.mod $(BIN)/.commit
	@mkdir -p $(BIN)
	go build -ldflags '$(LDFLAGS)' -o $@ ./cmd/specd

$(BIN)/hydradb-bins: $(shell find cmd/hydradb-bins -name '*.go') go.mod
	@mkdir -p $(BIN)
	go build -o $@ ./cmd/hydradb-bins

# The APIResourceSchemas are generated from the CRDs; a test fails when they
# drift, and this target is how a deliberate CRD change is regenerated.
generate-schemas:
	SPECD_UPDATE_GOLDEN=1 go test ./impl/schemagen/ -run TestEveryCRDHasItsAPIResourceSchema -count=1

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

LIVE_MODEL_LOG ?= .kcp-specd/live-model-tests.log

test-live-model:
	mkdir -p $(dir $(LIVE_MODEL_LOG))
	SPECD_REQUIRE_LIVE=1 SPECD_REQUIRE_LIVE_MODEL=1 go test ./test/e2e/ -count=1 -v -run 'LiveModel|PiHost|ScopeGuard' 2>&1 | tee $(LIVE_MODEL_LOG) | grep -E '^(=== RUN|--- (PASS|FAIL|SKIP)|PASS|FAIL|ok)'

# Every live test package starts its own kcp on kernel-assigned ports (state in
# a temporary root), so two of these run at once and neither touches the
# .kcp-specd cluster `make kcp-up` started. SPECD_E2E_KUBECONFIG=<admin
# kubeconfig> names an existing one instead, and then the live lock serialises
# runs that share it.

# Kernel-assigned ports by default, written to .kcp-specd/endpoint.json, so
# several of these run side by side. KCP_SECURE_PORT=6447 pins the old port.
kcp-up: $(SPECCTL)
	./deploy/start-kcp.sh

kcp-down: $(SPECCTL)
	./deploy/stop-kcp.sh

install-specs:
	./deploy/install-specs.sh

# The multi workspace half: the provider workspace, the generated
# APIResourceSchemas and the APIExport. deploy/bind-workspace.sh <ws> binds a
# tenant to it.
install-specs-provider:
	./deploy/install-specs-provider.sh

example-phase1: $(SPECCTL) kcp-up
	$(SPECCTL) apply -f examples/calc/specs.yaml
	$(SPECCTL) get systemcontext
	@echo "--- the same objects through kubectl, on the workspace kubeconfig ---"
	KUBECONFIG=$(WORKSPACE_KUBECONFIG) kubectl get systemcontexts
	@echo "--- one SystemContext as YAML ---"
	$(SPECCTL) get systemcontext calc -o yaml

example-phase2: $(SPECCTL) $(SPECD) kcp-up
	./scripts/example-phase2.sh

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

example-phase6: $(SPECCTL) $(SPECD) kcp-up
	./scripts/example-phase6.sh

# One manifest populates an unknown codebase: a bare git repository of two
# fixtures, cloned, indexed and summarized with no other command.
example-phase7: $(SPECCTL) $(SPECD) kcp-up
	./scripts/example-phase7.sh

# The mod path, deterministically: the context document rendered, the edit a
# model would make applied as a Go-computed delta, the queue behind a running
# change, and a progress report — the three verbs cc-clm-mod runs.
example-phase8: $(SPECCTL) $(SPECD) kcp-up
	./scripts/example-phase8.sh

# Two tenants, one APIExport, one specd in export mode, and the orphan open-architecture branch
# with its conflict rule.
example-phase9: $(SPECCTL) $(SPECD) kcp-up
	./scripts/example-phase9.sh

# The failing verify path of the same example: the change ends Failed with its
# branch kept and the managed branch untouched.
example-phase6-failing: $(SPECCTL) $(SPECD) kcp-up
	FAILING=1 MAX_ATTEMPTS=1 ./scripts/example-phase6.sh

# The phase 10 demo: the shortest path through the whole loop against one
# cluster. kcp up, one Repository manifest for a working tree, the code spelled
# out as specs, a spec edit, the delta, the agent commit gated by tests, the
# effectiveness table for the scripted baseline, and one scenario driven
# through the CLM path. SPECD_AGENT=claude runs the body with the live model,
# SPECD_CLM_PATH=off skips the closing scenario, SPECD_EVAL_OUT=<path> also
# writes the report.

example-phase13: build
	bash scripts/example-phase13.sh

demo: $(SPECCTL) $(SPECD) kcp-up
	./scripts/demo.sh

# Every phase, in order, against one cluster. It takes a few minutes: phases 2
# and 3 index a tree and import a 166 node document, and each example starts and
# stops its own controllers.
demo-phases: $(SPECCTL) $(SPECD) kcp-up
	@echo "=== phase 1: kcp holds specs ==="
	$(MAKE) --no-print-directory example-phase1
	@echo "=== phase 2: code -> facts -> kcp status + graph ==="
	$(MAKE) --no-print-directory example-phase2
	@echo "=== phase 3: the open architecture document ==="
	$(MAKE) --no-print-directory example-phase3
	@echo "=== phase 4: the controller, conditions and drift ==="
	$(MAKE) --no-print-directory example-phase4
	@echo "=== phase 5: code -> spec with a model ==="
	$(MAKE) --no-print-directory example-phase5
	@echo "=== phase 6: spec -> code, driven by the delta ==="
	$(MAKE) --no-print-directory example-phase6
	@echo "=== the same change, with a verify command that rejects it ==="
	$(MAKE) --no-print-directory example-phase6-failing
	@echo "=== one manifest populates an unknown codebase ==="
	$(MAKE) --no-print-directory example-phase7
	@echo "=== the CLM mod path: render, apply, fold, report ==="
	$(MAKE) --no-print-directory example-phase8
	@echo "=== two tenants, one export mode controller, and the git mirror ==="
	$(MAKE) --no-print-directory example-phase9

clean:
	rm -f $(SPECCTL) $(SPECD) $(BIN)/hydradb-bins
