package db

import (
	"context"
	"database/sql"

	"github.com/cockroachdb/errors"
	"github.com/kenners/zotrem/pkg/db/internal/sqlite"
	"github.com/kenners/zotrem/pkg/ts"
)

const (
	StateAnnotated = "annotated"
	StateError     = "error"
	StateQueued    = "queued"
	StateRemoved   = "removed"
	StateSynced    = "synced"
)

type Document struct {
	AnnotatedKey    string
	AnnotatedMD5    string
	AnnotationCount int
	CreatedAt       ts.Timestamp
	Filename        string
	ID              int64
	LastError       string
	LastPulledAt    *ts.Timestamp
	LastPushedAt    *ts.Timestamp
	RemarkableHash  string
	RemarkableID    string
	SceneSignature  string
	State           string
	Title           string
	UpdatedAt       ts.Timestamp
	ZoteroKey       string
	ZoteroMD5       string
	ZoteroParentKey string
}

type NewDocument struct {
	Filename        string
	Title           string
	ZoteroKey       string
	ZoteroMD5       string
	ZoteroParentKey string
}

var ErrDocumentNotFound = errors.New("document not found")

func IsDocumentNotFound(err error) bool { return errors.Is(err, ErrDocumentNotFound) }

func (d *DB) InsertDocument(n NewDocument) (Document, error) {
	now := ts.Now()
	row, err := d.queries.InsertDocument(context.Background(), sqlite.InsertDocumentParams{
		CreatedAt:       now,
		Filename:        n.Filename,
		State:           StateQueued,
		Title:           n.Title,
		UpdatedAt:       now,
		ZoteroKey:       n.ZoteroKey,
		ZoteroMd5:       n.ZoteroMD5,
		ZoteroParentKey: n.ZoteroParentKey,
	})
	if err != nil {
		return Document{}, errors.Wrap(err, "insert document")
	}
	return fromSqliteDocument(row), nil
}

func (d *DB) Document(id int64) (Document, error) {
	row, err := d.queries.GetDocument(context.Background(), id)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, errors.Wrap(err, "get document")
	}
	return fromSqliteDocument(row), nil
}

func (d *DB) DocumentByZoteroKey(key string) (Document, error) {
	row, err := d.queries.GetDocumentByZoteroKey(context.Background(), key)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, errors.Wrap(err, "get document by zotero key")
	}
	return fromSqliteDocument(row), nil
}

func (d *DB) DocumentByRemarkableID(id string) (Document, error) {
	row, err := d.queries.GetDocumentByRemarkableID(context.Background(), id)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, ErrDocumentNotFound
	}
	if err != nil {
		return Document{}, errors.Wrap(err, "get document by remarkable id")
	}
	return fromSqliteDocument(row), nil
}

func (d *DB) ListDocuments() ([]Document, error) {
	rows, err := d.queries.ListDocuments(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "list documents")
	}
	out := make([]Document, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromSqliteDocument(row))
	}
	return out, nil
}

func (d *DB) CountDocumentsByState() (map[string]int, error) {
	rows, err := d.queries.CountDocumentsByState(context.Background())
	if err != nil {
		return nil, errors.Wrap(err, "count documents")
	}
	out := map[string]int{}
	for _, row := range rows {
		out[row.State] = int(row.Count)
	}
	return out, nil
}

func (d *DB) UpdateDocument(doc Document) (Document, error) {
	doc.UpdatedAt = ts.Now()
	n, err := d.queries.UpdateDocument(context.Background(), sqlite.UpdateDocumentParams{
		AnnotatedKey:    doc.AnnotatedKey,
		AnnotatedMd5:    doc.AnnotatedMD5,
		AnnotationCount: int64(doc.AnnotationCount),
		Filename:        doc.Filename,
		ID:              doc.ID,
		LastError:       doc.LastError,
		LastPulledAt:    doc.LastPulledAt,
		LastPushedAt:    doc.LastPushedAt,
		RemarkableHash:  doc.RemarkableHash,
		RemarkableID:    doc.RemarkableID,
		SceneSignature:  doc.SceneSignature,
		State:           doc.State,
		Title:           doc.Title,
		UpdatedAt:       doc.UpdatedAt,
		ZoteroMd5:       doc.ZoteroMD5,
		ZoteroParentKey: doc.ZoteroParentKey,
	})
	if err != nil {
		return Document{}, errors.Wrap(err, "update document")
	}
	if n == 0 {
		return Document{}, ErrDocumentNotFound
	}
	return doc, nil
}

func (d *DB) DeleteDocument(id int64) error {
	n, err := d.queries.DeleteDocument(context.Background(), id)
	if err != nil {
		return errors.Wrap(err, "delete document")
	}
	if n == 0 {
		return ErrDocumentNotFound
	}
	return nil
}

func fromSqliteDocument(r sqlite.Document) Document {
	return Document{
		AnnotatedKey:    r.AnnotatedKey,
		AnnotatedMD5:    r.AnnotatedMd5,
		AnnotationCount: int(r.AnnotationCount),
		CreatedAt:       r.CreatedAt,
		Filename:        r.Filename,
		ID:              r.ID,
		LastError:       r.LastError,
		LastPulledAt:    r.LastPulledAt,
		LastPushedAt:    r.LastPushedAt,
		RemarkableHash:  r.RemarkableHash,
		RemarkableID:    r.RemarkableID,
		SceneSignature:  r.SceneSignature,
		State:           r.State,
		Title:           r.Title,
		UpdatedAt:       r.UpdatedAt,
		ZoteroKey:       r.ZoteroKey,
		ZoteroMD5:       r.ZoteroMd5,
		ZoteroParentKey: r.ZoteroParentKey,
	}
}

type Annotation struct {
	CreatedAt     ts.Timestamp
	DocumentID    int64
	Kind          string
	RmKey         string
	Signature     string
	ZoteroKey     string
	ZoteroVersion int
}

func (d *DB) ListAnnotations(documentID int64) ([]Annotation, error) {
	rows, err := d.queries.ListAnnotations(context.Background(), documentID)
	if err != nil {
		return nil, errors.Wrap(err, "list annotations")
	}
	out := make([]Annotation, 0, len(rows))
	for _, r := range rows {
		out = append(out, Annotation{
			CreatedAt:     r.CreatedAt,
			DocumentID:    r.DocumentID,
			Kind:          r.Kind,
			RmKey:         r.RmKey,
			Signature:     r.Signature,
			ZoteroKey:     r.ZoteroKey,
			ZoteroVersion: int(r.ZoteroVersion),
		})
	}
	return out, nil
}

func (d *DB) UpsertAnnotation(a Annotation) error {
	err := d.queries.UpsertAnnotation(context.Background(), sqlite.UpsertAnnotationParams{
		CreatedAt:     ts.Now(),
		DocumentID:    a.DocumentID,
		Kind:          a.Kind,
		RmKey:         a.RmKey,
		Signature:     a.Signature,
		ZoteroKey:     a.ZoteroKey,
		ZoteroVersion: int64(a.ZoteroVersion),
	})
	return errors.Wrap(err, "upsert annotation")
}

func (d *DB) DeleteAnnotation(documentID int64, rmKey string) error {
	return errors.Wrap(d.queries.DeleteAnnotation(context.Background(), sqlite.DeleteAnnotationParams{DocumentID: documentID, RmKey: rmKey}), "delete annotation")
}

func (d *DB) DeleteAnnotationsForDocument(documentID int64) error {
	return errors.Wrap(d.queries.DeleteAnnotationsForDocument(context.Background(), documentID), "delete annotations")
}
