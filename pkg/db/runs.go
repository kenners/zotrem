package db

import (
	"context"
	"database/sql"

	"github.com/cockroachdb/errors"
	"github.com/kenners/zotrem/pkg/db/internal/sqlite"
	"github.com/kenners/zotrem/pkg/ts"
)

const (
	RunStatusError   = "error"
	RunStatusOK      = "ok"
	RunStatusRunning = "running"
)

const (
	TriggerManual    = "manual"
	TriggerScheduled = "scheduled"
)

const (
	LevelError = "error"
	LevelInfo  = "info"
	LevelWarn  = "warn"
)

type SyncRun struct {
	Error      string
	FinishedAt *ts.Timestamp
	ID         int64
	Pulled     int
	Pushed     int
	Removed    int
	StartedAt  ts.Timestamp
	Status     string
	Trigger    string
}

type SyncEvent struct {
	CreatedAt  ts.Timestamp
	DocumentID int64
	ID         int64
	Level      string
	Message    string
	RunID      int64
}

var ErrRunNotFound = errors.New("sync run not found")

func (d *DB) StartSyncRun(trigger string) (SyncRun, error) {
	row, err := d.queries.InsertSyncRun(context.Background(), sqlite.InsertSyncRunParams{StartedAt: ts.Now(), Status: RunStatusRunning, Trigger: trigger})
	if err != nil {
		return SyncRun{}, errors.Wrap(err, "insert sync run")
	}
	return fromSqliteRun(row), nil
}

func (d *DB) FinishSyncRun(run SyncRun) error {
	err := d.queries.FinishSyncRun(context.Background(), sqlite.FinishSyncRunParams{
		Error:      run.Error,
		FinishedAt: ts.NowPtr(),
		ID:         run.ID,
		Pulled:     int64(run.Pulled),
		Pushed:     int64(run.Pushed),
		Removed:    int64(run.Removed),
		Status:     run.Status,
	})
	if err != nil {
		return errors.Wrap(err, "finish sync run")
	}
	return errors.Wrap(d.queries.PruneSyncRuns(context.Background(), 200), "prune sync runs")
}

func (d *DB) SyncRun(id int64) (SyncRun, error) {
	row, err := d.queries.GetSyncRun(context.Background(), id)
	if errors.Is(err, sql.ErrNoRows) {
		return SyncRun{}, ErrRunNotFound
	}
	if err != nil {
		return SyncRun{}, errors.Wrap(err, "get sync run")
	}
	return fromSqliteRun(row), nil
}

func (d *DB) LatestSyncRun() (*SyncRun, error) {
	row, err := d.queries.LatestSyncRun(context.Background())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "latest sync run")
	}
	run := fromSqliteRun(row)
	return &run, nil
}

func (d *DB) ListSyncRuns(limit int) ([]SyncRun, error) {
	rows, err := d.queries.ListSyncRuns(context.Background(), int64(limit))
	if err != nil {
		return nil, errors.Wrap(err, "list sync runs")
	}
	out := make([]SyncRun, 0, len(rows))
	for _, row := range rows {
		out = append(out, fromSqliteRun(row))
	}
	return out, nil
}

func (d *DB) AddSyncEvent(runID int64, documentID int64, level, message string) error {
	docID := sql.NullInt64{}
	if documentID != 0 {
		docID = sql.NullInt64{Int64: documentID, Valid: true}
	}
	err := d.queries.InsertSyncEvent(context.Background(), sqlite.InsertSyncEventParams{
		CreatedAt:  ts.Now(),
		DocumentID: docID,
		Level:      level,
		Message:    message,
		RunID:      runID,
	})
	return errors.Wrap(err, "insert sync event")
}

func (d *DB) ListSyncEvents(runID int64) ([]SyncEvent, error) {
	rows, err := d.queries.ListSyncEvents(context.Background(), runID)
	if err != nil {
		return nil, errors.Wrap(err, "list sync events")
	}
	return fromSqliteEvents(rows), nil
}

func (d *DB) ListDocumentEvents(documentID int64, limit int) ([]SyncEvent, error) {
	rows, err := d.queries.ListDocumentEvents(context.Background(), sqlite.ListDocumentEventsParams{DocumentID: sql.NullInt64{Int64: documentID, Valid: true}, Limit: int64(limit)})
	if err != nil {
		return nil, errors.Wrap(err, "list document events")
	}
	return fromSqliteEvents(rows), nil
}

func fromSqliteEvents(rows []sqlite.SyncEvent) []SyncEvent {
	out := make([]SyncEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, SyncEvent{
			CreatedAt:  r.CreatedAt,
			DocumentID: r.DocumentID.Int64,
			ID:         r.ID,
			Level:      r.Level,
			Message:    r.Message,
			RunID:      r.RunID,
		})
	}
	return out
}

func fromSqliteRun(r sqlite.SyncRun) SyncRun {
	return SyncRun{
		Error:      r.Error,
		FinishedAt: r.FinishedAt,
		ID:         r.ID,
		Pulled:     int(r.Pulled),
		Pushed:     int(r.Pushed),
		Removed:    int(r.Removed),
		StartedAt:  r.StartedAt,
		Status:     r.Status,
		Trigger:    r.Trigger,
	}
}
