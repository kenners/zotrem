-- name: InsertDocument :one
INSERT INTO documents (created_at, filename, state, title, updated_at, zotero_key, zotero_md5, zotero_parent_key)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetDocument :one
SELECT * FROM documents WHERE id = ?;

-- name: GetDocumentByZoteroKey :one
SELECT * FROM documents WHERE zotero_key = ?;

-- name: GetDocumentByRemarkableID :one
SELECT * FROM documents WHERE remarkable_id = ?;

-- name: ListDocuments :many
SELECT * FROM documents ORDER BY updated_at DESC, id DESC;

-- name: CountDocumentsByState :many
SELECT state, COUNT(*) AS count FROM documents GROUP BY state;

-- name: UpdateDocument :execrows
UPDATE documents SET
  annotated_key = ?,
  annotated_md5 = ?,
  annotation_count = ?,
  filename = ?,
  last_error = ?,
  last_pulled_at = ?,
  last_pushed_at = ?,
  remarkable_hash = ?,
  remarkable_id = ?,
  scene_signature = ?,
  state = ?,
  title = ?,
  updated_at = ?,
  zotero_md5 = ?,
  zotero_parent_key = ?
WHERE id = ?;

-- name: DeleteDocument :execrows
DELETE FROM documents WHERE id = ?;
