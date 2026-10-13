@AGENTS.md

# Repo rules

- `README.md` states what the system is and how to run it. Read it before a change.
- Go is primary. Layering follows `../kcp-libs`: `common/` <- `abc/` <- `impl/` <- `factory/` <- `cmd/`.
  `abc/` has no I/O. Arrow points one way only.
- The org-root `../CLAUDE.md` TypeScript rules apply to TypeScript code
  (`pi-hydradb-clm/`), its layering principles apply everywhere.
- kcp and kine take kernel-assigned ports; `specctl kcp start --root <dir>`
  writes them to `<dir>/endpoint.json` and state defaults to `.kcp-specd/`.
  `make kcp-up` / `make kcp-down` and `go test ./test/e2e` (private kcp per run)
  go through that; `KCP_SECURE_PORT`/`KINE_ENDPOINT` still pin a fixed port.
  Never stop or touch kcp/kine instances you did not start: other roots, other
  ports (6443/23791 belong to deno-kcp, 6447/23797 to an older instance of this
  repo) and their state directories are off limits.
- The graph is per instance, like kcp: `specctl up`, a standalone `specd` and
  every live test start their own HydraDB `graph-node` (`bin/graph-node`, lib in
  `bin/lib`, `make build` fetches both) on port 0 and record the bound address
  (`<kcp root>/graph/graph.json`, the session, `Repository.status.graph`). Never
  assume a fixed graph port; an external graph is named with `--bolt-url`.
- Every change: `gofmt`, `go vet ./...`, `go test ./...` green, then commit and push.
  The live suite is `make test-live` (`-p 6 -timeout 45m`): `test/e2e` runs
  longer than go test's default 10 minutes, and a timeout panic skips its
  cleanup. kcpproc caps instances host-wide at `SPECD_MAX_INSTANCES` (default
  max(2, CPUs/2)); a start waits for a free slot. Never leave an instance
  running: `specctl down` stops its kcp, kine and graph-node.
- Never edit outside this repo.
- `fixtures/` are sample codebases standing in for other people's code. Keep
  them realistic: their comments, docstrings and style are input to the code ->
  spec measurement, so the no-comments rule does not apply to them.
- Testing and evals use DeepSeek only (`deepseek-claude`, pi with
  `--provider deepseek`); never run a local model.
