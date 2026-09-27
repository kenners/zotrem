-- name: GetRemarkableItem :one
SELECT * FROM remarkable_items WHERE id = ?;

-- name: UpsertRemarkableItem :exec
INSERT INTO remarkable_items (file_type, hash, id, item_type, last_modified, parent, pinned, updated_at, visible_name)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  file_type = excluded.file_type,
  hash = excluded.hash,
  item_type = excluded.item_type,
  last_modified = excluded.last_modified,
  parent = excluded.parent,
  pinned = excluded.pinned,
  updated_at = excluded.updated_at,
  visible_name = excluded.visible_name;

-- name: DeleteRemarkableItems :exec
DELETE FROM remarkable_items;
