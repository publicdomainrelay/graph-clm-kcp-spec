@AGENTS.md

# Repo rules

- Active plan: `docs/plans/0001-kcp.md`. Read it before any change.
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
- HydraDB runs on bolt://127.0.0.1:7687, token in `/tmp/hdb/token`. ArcadeDB on 7688.
- Every change: `gofmt`, `go vet ./...`, `go test ./...` green, then commit and push.
- Never edit outside this repo.
- `fixtures/` are sample codebases standing in for other people's code. Keep
  them realistic: their comments, docstrings and style are input to the code ->
  spec measurement, so the no-comments rule does not apply to them.
- Testing and evals use DeepSeek only (`deepseek-claude`, pi with
  `--provider deepseek`); never run a local model.
