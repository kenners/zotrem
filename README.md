# zotrem

Two-way sync between a [Zotero](https://www.zotero.org) library and a [reMarkable](https://remarkable.com) tablet.

- Tag a paper in Zotero with `send-to-remarkable` (the tag is configurable) and its PDF appears in a `Zotero` folder
  on the tablet at the next sync.
- Read and annotate on the tablet. Pen strokes come back into Zotero as native **ink annotations** and reMarkable text
  highlights as **highlight annotations** on the original PDF, so they show up in the Zotero reader. A second copy of
  the PDF with the strokes burnt in is attached alongside for use in other viewers.
- Zotero tags mirror the state of each paper: `zotrem:synced`, `zotrem:annotated`, `zotrem:removed`, `zotrem:error`.
- Remove the tag in Zotero and the document moves to the reMarkable trash. Delete it on the tablet and the tag is
  cleared in Zotero.

zotrem is a single Go binary with a small web UI. It talks to the Zotero Web API and the reMarkable cloud, so it works
with PDFs stored in Zotero Storage without a running Zotero desktop client.

## Requirements

- A Zotero account with the PDFs stored in Zotero Storage (WebDAV storage is not supported) and a Zotero API key with
  library, write and notes access. PDFs kept as *linked files* (for example by ZotFile or ZotMoveFile) are not
  synced; a tagged paper with a linked PDF is marked as an error.
- A reMarkable tablet on firmware 3.x connected to the reMarkable cloud (a Connect subscription is not required).
- Somewhere to run zotrem: a laptop, a home server or a NAS. It keeps its state in a SQLite database.

## Install

Every push to `main` builds Linux and macOS binaries (amd64 and arm64) as a "zotrem-binaries" artifact on the
GitHub Actions run, and tagging `vX.Y.Z` publishes them as a release. Download the archive for your platform and
put `zotrem` on your `PATH`, or build from source:

```sh
go install github.com/kenners/zotrem/cmd/zotrem@latest
```

Or run the container image:

```sh
docker run -d --name zotrem -p 8000:8000 -v zotrem-data:/data ghcr.io/kenners/zotrem
```

## Run

```sh
zotrem serve --port 8000
```

Then open http://localhost:8000 and:

1. **Zotero** — paste an API key created at https://www.zotero.org/settings/keys/new with "Allow library access",
   "Allow write access" and "Allow notes access". zotrem verifies the key and finds your user ID. For a group library,
   choose "Group library" and enter the group ID.
2. **reMarkable** — open https://my.remarkable.com/device/browser/connect, sign in, and enter the one-time code.
3. **Sync options** — pick the tag, folder and sync interval.

Tag a paper in Zotero, press **Sync now** (or wait for the scheduler) and the PDF is on the tablet. After you have
annotated it on the tablet, the next sync brings the annotations into Zotero.

Other commands:

```sh
zotrem sync            # run one sync and exit (useful from cron)
zotrem pair CODE       # pair with the reMarkable cloud from the command line
zotrem serve --bind 0.0.0.0 --port 8000
```

Environment variables:

| Variable | Purpose |
| --- | --- |
| `ZOTREM_DATA_DIR` | Directory for `zotrem.db` (default: the user config directory, e.g. `~/.config/zotrem`). |

The web UI and API have no login of their own; bind them to localhost (the default) or a trusted private network.
The Zotero API key and reMarkable tokens are stored unencrypted in the database, so protect the data directory.

## How a sync works

Every run (manual, or every 15 minutes by default):

1. Papers carrying the send tag are looked up in Zotero. Each stored PDF attachment that is not yet on the tablet is
   downloaded and uploaded into the reMarkable folder; the paper is tagged `zotrem:synced`.
2. For every document on the tablet whose content changed since the last run, the page files are parsed. Strokes and
   highlights are converted into Zotero annotations on the original attachment (annotations that were erased on the
   tablet are removed again; comments you add in Zotero to an unchanged annotation survive), and the annotated PDF
   attachment is refreshed. The paper is tagged `zotrem:annotated`.
3. Documents whose paper no longer carries the send tag are moved to the reMarkable trash. Documents deleted on the
   tablet lose the send tag in Zotero and get `zotrem:removed`.

Per-document failures are recorded (and tagged `zotrem:error`) without stopping the run. See `docs/DESIGN.md` for the
details, including how reMarkable coordinates are mapped onto PDF pages.

## Monitoring

Paste a [Healthchecks.io](https://healthchecks.io) ping URL (or one from a self-hosted instance) under **Settings →
Monitoring**. Every sync signals its start, then success, or failure when the sync or any document fails; the failure
ping carries a short report with the error messages. Set the check's period to the sync interval so that a zotrem that
has stopped running is noticed too. **Send test ping** posts a log entry, which does not change the check's status.

## Limitations

- Notebook pages inserted between PDF pages on the tablet are skipped; only annotations on the PDF's own pages are
  transferred.
- Stroke placement has been verified on a reMarkable Paper Pro with PDFs opened in the default page fit.
- Typed text (the tablet's keyboard text tool) is not transferred.
- Replacing a PDF in Zotero after it has been sent does not re-send it; remove and re-add the tag to send the new file.
- The reMarkable cloud API is not officially documented; zotrem follows the protocol used by the community clients
  [rmapi](https://github.com/ddvk/rmapi) and [rmapi-js](https://github.com/erikbrinkman/rmapi-js) and may need updates
  when reMarkable changes it.

## Development

```sh
mage generate   # templ + sqlc + OpenAPI
mage build      # builds to ~/go/bin/zotrem
mage dev        # hot reload with air
go test ./...
```

Tests never contact Zotero or reMarkable; they run against in-memory fakes of both services. See `AGENTS.md` for the
conventions used in this repository.

## Acknowledgements

The `.rm` file parser follows the format documented by [rmscene](https://github.com/ricklupton/rmscene) (MIT) and uses
its test fixtures. Project scaffolding comes from [housecat-inc/scratch](https://github.com/housecat-inc/scratch).

## License

MIT
