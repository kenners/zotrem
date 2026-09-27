-- name: InsertSyncRun :one
INSERT INTO sync_runs (started_at, status, trigger) VALUES (?, ?, ?) RETURNING *;

-- name: FinishSyncRun :exec
UPDATE sync_runs SET error = ?, finished_at = ?, pulled = ?, pushed = ?, removed = ?, status = ? WHERE id = ?;

-- name: GetSyncRun :one
SELECT * FROM sync_runs WHERE id = ?;

-- name: ListSyncRuns :many
SELECT * FROM sync_runs ORDER BY started_at DESC, id DESC LIMIT ?;

-- name: LatestSyncRun :one
SELECT * FROM sync_runs ORDER BY started_at DESC, id DESC LIMIT 1;

-- name: PruneSyncRuns :exec
DELETE FROM sync_runs WHERE id NOT IN (SELECT id FROM sync_runs ORDER BY started_at DESC, id DESC LIMIT ?);

-- name: InsertSyncEvent :exec
INSERT INTO sync_events (created_at, document_id, level, message, run_id) VALUES (?, ?, ?, ?, ?);

-- name: ListSyncEvents :many
SELECT * FROM sync_events WHERE run_id = ? ORDER BY id;

-- name: ListDocumentEvents :many
SELECT * FROM sync_events WHERE document_id = ? ORDER BY id DESC LIMIT ?;
