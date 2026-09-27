-- name: ListAnnotations :many
SELECT * FROM annotations WHERE document_id = ? ORDER BY rm_key;

-- name: UpsertAnnotation :exec
INSERT INTO annotations (created_at, document_id, kind, rm_key, signature, zotero_key, zotero_version)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(document_id, rm_key) DO UPDATE SET
  kind = excluded.kind,
  signature = excluded.signature,
  zotero_key = excluded.zotero_key,
  zotero_version = excluded.zotero_version;

-- name: DeleteAnnotation :exec
DELETE FROM annotations WHERE document_id = ? AND rm_key = ?;

-- name: DeleteAnnotationsForDocument :exec
DELETE FROM annotations WHERE document_id = ?;
