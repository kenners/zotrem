-- name: ListSettings :many
SELECT * FROM settings ORDER BY key;

-- name: UpsertSetting :exec
INSERT INTO settings (key, updated_at, value) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET updated_at = excluded.updated_at, value = excluded.value;

-- name: DeleteSetting :exec
DELETE FROM settings WHERE key = ?;
