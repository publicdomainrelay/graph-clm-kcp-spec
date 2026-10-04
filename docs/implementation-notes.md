# Implementation notes

The code carries no comments: names and types are meant to carry the meaning,
and `docs/plans/0001-kcp.md` and `README.md` carry the design. This file carries
the rest — the facts a reader used to find in a comment and nowhere else, one
bullet per fact, grouped by package. If a bullet here ever stops being true, the
code is what changed.

## abc/agent

- `Fit` always keeps the first section, even when it alone exceeds the budget: a
  prompt without its contract is not a prompt.
- A `file:` ref already is its CodeGraph id, so a file ref enters the managed
  zone without a lookup; a symbol ref needs one.

## abc/clm

- `EmptyIntent` is a placeholder that maps back to an empty intent, so a render
  followed by a parse of an empty spec is a no-op.
- `MergeDeclared` takes a field the model omitted from the base, so an older
  document cannot erase a newer field.
- The fenced spec block omits `intent` on purpose: the prose is the single
  authority, and two spellings of it could disagree.

## abc/spec

- `CodeRefKinds` is the index's own vocabulary — a TypeScript interface is
  `interface:<hex>` and a class is `class:<hex>`, not `type:<hex>` — because a
  ref a model copied from an observed fact must be valid by construction. The
  parser and the validator share the whole list.
- A progress record's `At` is stamped by the host, so it says when the work
  happened, not when the tool heard about it.
- `progress` is bounded at 128 records because every watcher reads the status
  subresource; the newest are kept and the oldest evicted.
- The agent option is validated at apply time, so a typo is rejected instead of
  leaving changes silently `Pending`.
- `status.realizedSpec` is validated by the same rules as a live spec; its own
  status is empty, so the validation does not recurse.

## abc/sync

- `Roots`: `.` is always a root, and an empty list means the repository root
  only.
- Two symbols that share a key collapse to one, first in file and line order,
  because the API server refuses a status with a duplicate key.
- `MigrateDeclared` copies only when something actually moves: an empty list
  rebuilt non-nil would read as an edit and raise a change.
- `RetryBackoff`: a zero `lastAttempt` retries at once, `attempts == 0` is
  immediate, `maxAttempts == 0` is no cap, and the doubling is capped at
  `maxRetryBackoff`, ten minutes.

## abc/archyaml

- A node's `code` field is a path, a list of paths, or a name-to-path mapping. A
  path may carry `:SYMBOL`; a value that is not a path (a shell command, say) is
  dropped.
- Export inserts children left to right by slot index, so a re-inlined position
  matches the slot that was recorded on the way in.
- `equalJSON` compares through JSON, the same trip the kcp client makes.

## abc/delta

- An empty delta prints as a dash, because a rewrite that changed nothing is not
  work.

## abc/mirror, abc/sync

- `mirror.LegacyDir` is `.specs/`, the in-tree mirror of phase 9. Phase 13 moved
  the mirror to the orphan branch (`specs/<context>.yaml`), but a repository
  cloned from before then can still carry one. `sync.IsLegacySpecPath` keeps
  such a path out of every partition and out of the observed facts, even when
  the index happened to read one, because a spec the tool wrote must never look
  like code that drifted.

## impl/codegraphsqlite

- The TypeScript member-visibility scan reads the declaration line only up to
  the member's own name, so a constructor's `private` parameter property does
  not hide a public constructor.
- A declaration line that cannot be read leaves the member public, because that
  is the language default.
- `Resolve` never computes an id and tries a payload in a fixed order: the
  CodeGraph id, then the qualified name, then the bare name, then the file path.
- The reader accepts the spec's `Type.Method` and the index's `Type::Method` as
  one symbol, and resolves it there rather than through the bare-name fallback,
  which cannot tell two receivers apart.

## impl/ingest

- A context that ingest creates is its own upstream: the CRD default is applied
  by the API server, and the local validator runs before that.
- `SanitizeName` derives a DNS-1123 label so a manifest that names a path never
  carries a character the API server rejects.
- The package partition never descends into a VCS store, the CodeGraph index
  directory, or a dependency tree that a package manifest may live in.

## impl/codegraphcli, impl/bundle

- `codegraph context` output is markdown with code blocks and is the largest
  section of a bundle, so `Fit` drops it first under a budget.
- `codegraph node` reads one symbol's source plus its caller trail, for one code
  ref; a bundle asks for a few.
- `bundle` stats the CodeGraph database before spawning `codegraphcli`: with no
  index there is nothing to read, and spawning it twice per context is waste.
- `Neighbors` reads through the same graph edge table the writer uses, so reader
  and writer cannot disagree about an edge's direction.
- An empty model zone keeps the document's existing model zone, so a summarize
  that produced no prose does not erase the last summary. A missing document is
  not an error: the first summarize starts from empty.

## impl/claudecli, impl/realize

- `WaitDelay` bounds the wait after a timeout or a cancel, because a killed
  model or verify runner can leave a child holding the output pipes open and the
  run would otherwise hang past its timeout.
- `environ` replaces a name rather than appending it twice: which of two
  duplicate entries a child reads is undefined. An empty override set inherits
  the process environment unchanged.
- `realize` removes the worktree before the branch is landed: a branch a worktree
  still holds cannot be deleted, and the commit lives in the repository's refs,
  not in the worktree directory. The deferred call is the safety net for early
  returns.
- An empty verify command passes: a repository that names none has not asked for
  a gate.
- `realize.OutputTailBytes` (4000) bounds what a failed verify leaves in the
  `SpecChange`; `realize.ChangedFilesLimit` (100) bounds the diff stat carried in
  `status.filesTouched`.

## impl/schemagen

- An APIResourceSchema name is `<version>-<revision>.<plural>.<group>`. The
  object is immutable, so a changed CRD gets the next revision rather than an
  edit; `Revision` is 3, and `deploy/specs-apiexport.yaml` names the published
  revision with a test holding the two together. Revision 2 was the interface
  `name` descriptions keyed by receiver.

## impl/stripbodies

- Go: `panic` is a terminating statement, so a value-returning function still
  compiles after its body is replaced. TypeScript: `throw` is assignable to
  every declared return type, so a stripped module still type checks.
- A TypeScript arrow function whose body is a block is out of scope: telling one
  apart from a call needs a real parser, and the fixtures do not use one.

## impl/kcpclient

- An empty namespace means the client default everywhere, and
  `metav1.NamespaceAll` is itself the empty string, so the two cannot be told
  apart; a cluster-wide listing has its own entry point, `ListAll`.
- On a decode failure the unstructured converter reports only the reason
  ("cannot restore slice from string"), so the object is re-encoded through
  `encoding/json` purely to obtain the field path in the error.

## impl/runlock

- `Release` drops the flock but leaves the file on disk: the file is the meeting
  point, not the state.

## impl/gitrepo

- The clone cache is keyed by Repository, not by URL. A manifest whose url
  changed makes the controller delete and re-clone; otherwise it would fetch
  from the origin the cache was first cloned from and quietly index a different
  codebase.
- A ref that exists as `origin/<ref>` is force-reset onto it: a plain checkout
  leaves the cached local branch behind, so a ref source would be indexed once
  and never follow its remote again.
- A tag or a commit is checked out detached, because it cannot move. An empty
  ref means the remote default and is advanced with `merge --ff-only @{u}`,
  falling back to `origin/HEAD` when the checkout is detached.

## impl/clm

- `report` stamps a progress record's time in the bridge, not in the host: a
  host that shells out has no clock it can trust, and no reason to pass one.

## impl/agentfactory

- The pi extension folder reaches the agent through `SPECD_PI_EXTENSION`.
- `piArgs` appends `--extension` because pi discovers extensions from a settings
  file, not from the folder that holds them; without it the CLM path asks a
  model to edit a document nothing applies.
- `absolute()` resolves the plugin or extension folder before the model starts:
  the model runs in the working tree, not in the directory the flag was typed
  in, so a relative folder would be looked for inside the tree and would fail as
  a missing extension rather than as a wrong path.
- A `claude-mod` run's arguments are `claudecli.DefaultArgs()` plus
  `--plugin-dir`; `claudecli` fills its own defaults only when it is given none.

## impl/populate

- The status patch is the typed `RepositoryStatus` marshalled and unmarshalled,
  so fields this run has nothing to say about are omitted exactly as on the
  compared object. A hand-written map would carry zero values and make every
  reconcile look like a change.
- A change that was created but not yet phased counts as unfinished, because
  create and status patch are two calls; treating it as absent would raise a
  second attempt of the same episode beside it.
- `listChanges` reads every SpecChange because both directions share one name
  namespace, so a new change must not collide with one of the other direction.
- `WaitForPopulated` treats a wait timeout as a controller error rather than a
  manifest error, so the caller reports both.

## fixtures

- `fixtures/ledger`: the baseline `domain.Validate` accepts a negative amount
  and only asks for an account. The scenario that changes this
  (`scenarios/02-reject-negative-amounts.yaml`) states the before and after in
  its description, its spec patch and its acceptance test, and the baseline
  `summarize.yaml` deliberately says only that `Validate` rejects a missing
  account.
- `fixtures/todo`: `todo.Store`'s zero value is not usable; a store is made with
  `NewStore`, which is what gives the first task id 1.

## common/ids

- An id is masked to 53 bits so it survives a round trip through a JSON number
  and through the CLM graph: the same key gives the same id here and in
  `pi-hydradb-clm`. Keys are ASCII by construction.

## factory/specd

- Readiness costs one read and no worktree when neither the repository nor the
  controller names an agent.
- A change that names a deleted context or repository is left in place rather
  than marked `Failed`, because a failure only asks for it again.
- A realize branch carries the spec hash, so two edits of one context never
  share a branch.
- The worktree directory lives outside the managed tree; inside it, it would
  show up as untracked in the tree the commit is taken from.
- An agent lookup error answers "no", so a change stays `Pending` for a human
  instead of `Failed` on a read error.
- A create cannot write the status subresource, so the phase is patched in
  immediately after the create and no reader sees a phase-less change.
- The cache directory name is a DNS-1123 label by the time it is joined, so the
  join cannot escape the cache root.
- A quiet context is the common case and costs no List.
- An episode that never failed has no attempt recorded, so attempt 1 is
  immediate and the backoff starts at 2.
- A key that carries no cluster means a plain workspace client, which keeps
  single-workspace mode and the unit tests unaffected.

## cmd/specctl

- `--out run.json` names the JSON report; the markdown lands beside it, so
  neither overwrites the other.
- `ingest` still accepts the codegraph, no-graph, bundle and agent-timeout flags
  only so an older invocation parses; the controller owns them now. A flag that
  moved prints a "pass it to specd" note, so an established invocation is not
  silently half honoured.
- `addGlobalsExcept` leaves out the global `--context` where `clm render
  --context` names a SystemContext, because the two names would collide.
- `--agent-args` is shell-dequoted as a whole and then split on whitespace, so
  `"-p --verbose"` becomes two argv words.

## cmd/specd

- An explicit `--cache-dir` beats `SPECD_CACHE_DIR` even when it is the default
  value; the environment fills only an unnamed directory.

- `clm render --context <name>` (a SystemContext) sits beside the global
  `--context` (a kubeconfig). Registering the same flag twice panics, so the
  subcommand takes its own name and must not read the global one.
