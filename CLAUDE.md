@AGENTS.md

# Repo rules

- Active plan: `docs/plans/0001-kcp.md`. Read it before any change.
- Go is primary. Layering follows `../kcp-libs`: `common/` <- `abc/` <- `impl/` <- `factory/` <- `cmd/`.
  `abc/` has no I/O. Arrow points one way only.
- The org-root `../CLAUDE.md` TypeScript rules apply to TypeScript code
  (`pi-hydradb-clm/`), its layering principles apply everywhere.
- kcp for this repo runs on port 6447 with kine on 23797, state in `.kcp-specd/`.
  Never stop or touch kcp/kine instances on other ports (6443, 23791 belong to deno-kcp).
- HydraDB runs on bolt://127.0.0.1:7687, token in `/tmp/hdb/token`. ArcadeDB on 7688.
- Every change: `gofmt`, `go vet ./...`, `go test ./...` green, then commit and push.
- Never edit outside this repo.
