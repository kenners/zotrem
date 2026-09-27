-- +goose Up
CREATE TABLE settings (
  key        TEXT      PRIMARY KEY,
  updated_at TIMESTAMP NOT NULL,
  value      TEXT      NOT NULL
);

CREATE TABLE remarkable_items (
  file_type     TEXT      NOT NULL DEFAULT '',
  hash          TEXT      NOT NULL,
  id            TEXT      PRIMARY KEY,
  item_type     TEXT      NOT NULL,
  last_modified TEXT      NOT NULL DEFAULT '',
  parent        TEXT      NOT NULL DEFAULT '',
  pinned        INTEGER   NOT NULL DEFAULT 0,
  updated_at    TIMESTAMP NOT NULL,
  visible_name  TEXT      NOT NULL
);

CREATE TABLE documents (
  annotated_key     TEXT      NOT NULL DEFAULT '',
  annotated_md5     TEXT      NOT NULL DEFAULT '',
  annotation_count  INTEGER   NOT NULL DEFAULT 0,
  created_at        TIMESTAMP NOT NULL,
  filename          TEXT      NOT NULL,
  id                INTEGER   PRIMARY KEY AUTOINCREMENT,
  last_error        TEXT      NOT NULL DEFAULT '',
  last_pulled_at    TIMESTAMP,
  last_pushed_at    TIMESTAMP,
  remarkable_hash   TEXT      NOT NULL DEFAULT '',
  remarkable_id     TEXT      NOT NULL DEFAULT '',
  scene_signature   TEXT      NOT NULL DEFAULT '',
  state             TEXT      NOT NULL,
  title             TEXT      NOT NULL,
  updated_at        TIMESTAMP NOT NULL,
  zotero_key        TEXT      NOT NULL UNIQUE,
  zotero_md5        TEXT      NOT NULL DEFAULT '',
  zotero_parent_key TEXT      NOT NULL DEFAULT ''
);
CREATE INDEX idx_documents_remarkable_id ON documents(remarkable_id);
CREATE INDEX idx_documents_state ON documents(state);

CREATE TABLE annotations (
  created_at     TIMESTAMP NOT NULL,
  document_id    INTEGER   NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
  id             INTEGER   PRIMARY KEY AUTOINCREMENT,
  kind           TEXT      NOT NULL,
  rm_key         TEXT      NOT NULL,
  signature      TEXT      NOT NULL,
  zotero_key     TEXT      NOT NULL,
  zotero_version INTEGER   NOT NULL DEFAULT 0,
  UNIQUE (document_id, rm_key)
);

CREATE TABLE sync_runs (
  error       TEXT      NOT NULL DEFAULT '',
  finished_at TIMESTAMP,
  id          INTEGER   PRIMARY KEY AUTOINCREMENT,
  pulled      INTEGER   NOT NULL DEFAULT 0,
  pushed      INTEGER   NOT NULL DEFAULT 0,
  removed     INTEGER   NOT NULL DEFAULT 0,
  started_at  TIMESTAMP NOT NULL,
  status      TEXT      NOT NULL,
  trigger     TEXT      NOT NULL
);
CREATE INDEX idx_sync_runs_started_at ON sync_runs(started_at);

CREATE TABLE sync_events (
  created_at  TIMESTAMP NOT NULL,
  document_id INTEGER,
  id          INTEGER   PRIMARY KEY AUTOINCREMENT,
  level       TEXT      NOT NULL,
  message     TEXT      NOT NULL,
  run_id      INTEGER   NOT NULL REFERENCES sync_runs(id) ON DELETE CASCADE
);
CREATE INDEX idx_sync_events_run_id ON sync_events(run_id);

-- +goose Down
DROP TABLE IF EXISTS sync_events;
DROP TABLE IF EXISTS sync_runs;
DROP TABLE IF EXISTS annotations;
DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS remarkable_items;
DROP TABLE IF EXISTS settings;
