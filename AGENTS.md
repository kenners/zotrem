# AGENTS

You are an experienced software engineer and architect. Communicate with brevity. Be persistent and creative.

zotrem syncs PDFs tagged in a Zotero library to a reMarkable tablet via the reMarkable cloud and brings the
annotations made on the tablet back into Zotero. Read `docs/DESIGN.md` before changing sync behaviour.

## Stack

- Idiomatic Go, single binary, SQLite state
- HTML via templ; HTMX for interactivity; vanilla JS sparingly
- Static assets (JS, CSS, icons) live in `pkg/ui/static` and are served from an `//go:embed` filesystem

## Style

- Write self-commenting code; do not write comments
- Sort things lexicographically — struct fields, DB columns, map keys, switch cases
- Errors: `github.com/cockroachdb/errors`
- Tests: table-driven, `github.com/stretchr/testify` with `a := assert.New(t)` and `r := require.New(t)`
- Never talk to the real Zotero or reMarkable services from tests; use the fakes in `pkg/zotero/zoterotest` and
  `pkg/remarkable/remarkabletest`

## Build & codegen

Build and dev tasks run through [mage](https://magefile.org); Go tools are invoked with `go tool`:

- `mage build` — build `cmd/zotrem`
- `mage dev` — hot-reload dev server via air
- `mage generate` — run all code generation (sqlc and templ first, then the OpenAPI spec, which imports the generated packages; plain `go generate ./...` fails on a clean checkout)
- `go test ./...` — run tests (browser tests need Chromium; set `ROD_BROWSER_BIN` if it is not on a standard path)

Never hand-edit generated code. `mage build` regenerates before compiling; edit the source instead:

- `*.templ` → templ (`*_templ.go`) — gitignored
- `pkg/db/queries/*.sql`, `pkg/db/schema/*.sql` → sqlc (`pkg/db/internal/sqlite`) — gitignored
- API route definitions → OpenAPI (`docs/openapi.json`) — committed as the API contract

## Nix

- `flake.nix` builds the package (`nix/package.nix`), the NixOS module (`nix/module.nix`) and a VM test (`nix/test.nix`)
- After changing `go.mod` or `go.sum`, update `vendorHash` in `flake.nix`: set it to `nixpkgs.lib.fakeHash`, run
  `nix build .#zotrem` and copy the hash from the error
- `nix flake check` runs the Go tests and the VM test (needs KVM)

## Database

- SQLite via sqlc — queries in `pkg/db/queries`, schema in `pkg/db/schema`
- Migrations are goose files named `NNNNN_description.sql`, numbered sequentially
- Timestamp columns map to `ts.Timestamp` from `pkg/ts` (RFC3339)

## Version control

- Start every change on a new branch off `main`; never commit to `main` directly
- Commit after every turn so progress is reviewable — small commits are fine

## Pull requests

Format as customer-facing release notes:

- Title under 80 characters
- Body is a markdown list — every non-empty line starts with `-`, `*`, `+`, or `N.`
- Summarize user-facing capability, not implementation
