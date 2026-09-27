# zotrem design

zotrem is a self-hosted web app that keeps a reMarkable tablet in step with a Zotero library.

## Goals

- Papers tagged in Zotero with a user-chosen tag (default `send-to-remarkable`) appear as PDFs in a folder on the
  reMarkable (default folder `Zotero`).
- Annotations made on the tablet come back into Zotero, attached to the original PDF attachment:
  - pen strokes become Zotero `ink` annotations and reMarkable text highlights become Zotero `highlight` annotations,
    so they show in the Zotero reader and are searchable;
  - an "(annotated)" PDF with the strokes burnt in is added as a sibling attachment for use outside Zotero.
- Zotero tags reflect sync state so the user can see it from any Zotero client.
- Removing the tag in Zotero moves the document to the reMarkable trash; deleting it on the tablet removes the tag.

## Components

| Package | Responsibility |
| --- | --- |
| `pkg/zotero` | Zotero Web API v3 client: search by tag, children, file download, three-step file upload, item PATCH, annotation items. |
| `pkg/remarkable` | reMarkable cloud client: device pairing, token refresh, root-hash sync API (schema 3/4 indexes, content-addressed blobs), put/move/trash documents. |
| `pkg/rm` | Parser for reMarkable `.rm` v6 (firmware 3.x) scene files: tagged blocks, CRDT ordering, strokes, text highlights, layers. |
| `pkg/render` | Geometry from reMarkable scene units to PDF user space; Zotero annotation payloads; annotated PDF (stroke overlay stamped onto the original). |
| `pkg/sync` | The sync engine and scheduler. |
| `pkg/db` | SQLite state (settings, credentials, document mappings, annotation mappings, run log). |
| `pkg/web`, `pkg/ui`, `pkg/api` | HTTP UI (templ + HTMX), REST API (fuego, OpenAPI in `docs/openapi.json`). |

## Sync algorithm

Every run (manual "Sync now" or the scheduler, default every 15 minutes):

1. **Discover** — `GET /users/{id}/items?tag={send}` (top-level items and directly tagged attachments). For each
   regular item fetch `/children` and keep PDF attachments stored in Zotero (`imported_file`, `imported_url`).
2. **Push** — for every tagged PDF without a mapping: download the file, upload it into the reMarkable folder with
   the low-level put API (new document id, `.content`, `.metadata`, `.pagedata`, `.pdf`, document index, root index,
   `PUT /sync/v3/root` with the current generation), store the mapping, tag the parent `zotrem:synced`.
3. **Pull** — read the root index once (item metadata is cached by hash in SQLite, so unchanged items cost no
   requests). For every mapped document whose index hash changed: fetch `.content` and the tablet's copy of the PDF,
   resolve page order (`cPages` sorted by `idx`, skipping deleted, `redir` → original PDF page; legacy `pages` +
   `redirectionPageMap`), parse each page's `.rm`, project strokes and highlights into PDF coordinates, then compute a
   signature of the result; when the signature is unchanged (the document was merely opened) nothing is written. Else
   - diff the resulting annotation set against the annotations zotrem previously created (keyed by the CRDT id of the
     stroke/highlight) and create/delete Zotero annotation items accordingly (user edits to comments survive);
   - regenerate the annotated PDF (stroke overlay drawn with fpdf, stamped onto the original with pdfcpu) and upload it
     as `<name> (reMarkable).pdf` (created once, then replaced with `If-Match`);
   - tag the parent `zotrem:annotated`, store the new hash.
   Documents that are gone from the root index or sit in `trash` are marked removed: the send tag is removed and
   `zotrem:removed` added.
4. **Untag** — mapped documents whose Zotero parent no longer carries the send tag are moved to the reMarkable trash,
   `zotrem:synced` is removed and the mapping is marked removed.

Failures are recorded per document (`zotrem:error` tag, error text in the UI) and never abort the run.

## Geometry

reMarkable v6 scene coordinates are pixels of the source page at a fixed 226 units per inch: x is centred on the
page (`0` is the page centre, negative to the left) and y grows downward from the page top. A PDF page of `W × H`
points therefore spans `-W·226/144 … W·226/144` horizontally. zotrem maps a scene point `(x, y)` on a page whose
displayed CropBox is `W × H` to PDF user space `(llx + W/2 + x·72/226, lly + H − y·72/226)`, then undoes the page
`/Rotate`. Zoom modes do not change stored coordinates. The 226 scale was verified live on a reMarkable Paper Pro
(whose screen is 229 dpi), so it does not depend on the device model.

## State tags

All state tags share a configurable prefix (default `zotrem:`): `synced`, `annotated`, `removed`, `error`.

## Credentials

The Zotero API key and reMarkable tokens are stored in the SQLite database in the data directory. The web UI has no
authentication of its own; run it on a trusted network or behind a reverse proxy, or set `ZOTREM_PASSWORD` to require
HTTP basic auth.
