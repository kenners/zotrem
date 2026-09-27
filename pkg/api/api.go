package api

//go:generate go run ../../cmd/openapi ../../docs/openapi.json

import (
	"time"

	"github.com/cockroachdb/errors"
	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-fuego/fuego"
	"github.com/go-fuego/fuego/option"
	"github.com/kenners/zotrem/pkg/db"
	"github.com/kenners/zotrem/pkg/sync"
	"github.com/kenners/zotrem/pkg/ts"
)

type Status struct {
	Configured bool           `json:"configured"`
	Counts     map[string]int `json:"counts"`
	LastRun    *Run           `json:"lastRun,omitempty"`
	NextRunAt  *time.Time     `json:"nextRunAt,omitempty"`
	Running    bool           `json:"running"`
	Zotero     bool           `json:"zotero"`
	Remarkable bool           `json:"remarkable"`
	Version    string         `json:"version"`
}

type Document struct {
	AnnotatedKey    string     `json:"annotatedKey,omitempty"`
	AnnotationCount int        `json:"annotationCount"`
	Filename        string     `json:"filename"`
	ID              int64      `json:"id"`
	LastError       string     `json:"lastError,omitempty"`
	LastPulledAt    *time.Time `json:"lastPulledAt,omitempty"`
	LastPushedAt    *time.Time `json:"lastPushedAt,omitempty"`
	RemarkableID    string     `json:"remarkableId,omitempty"`
	State           string     `json:"state"`
	Title           string     `json:"title"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	ZoteroKey       string     `json:"zoteroKey"`
	ZoteroParentKey string     `json:"zoteroParentKey,omitempty"`
}

type Run struct {
	Error      string     `json:"error,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	ID         int64      `json:"id"`
	Pulled     int        `json:"pulled"`
	Pushed     int        `json:"pushed"`
	Removed    int        `json:"removed"`
	StartedAt  time.Time  `json:"startedAt"`
	Status     string     `json:"status"`
	Trigger    string     `json:"trigger"`
}

type SyncResponse struct {
	Started bool   `json:"started"`
	Message string `json:"message"`
}

type Deps struct {
	Engine  *sync.Engine
	Store   *db.DB
	Version string
}

type handlers struct {
	deps Deps
}

func Register(s *fuego.Server, deps Deps) {
	h := handlers{deps: deps}
	fuego.Get(s, "/api/status", h.status, option.Summary("Sync status"), option.Tags("status"))
	fuego.Get(s, "/api/documents", h.documents, option.Summary("List synced documents"), option.Tags("documents"))
	fuego.Get(s, "/api/runs", h.runs, option.Summary("List recent sync runs"), option.Tags("runs"))
	fuego.Post(s, "/api/sync", h.sync, option.Summary("Start a sync run"), option.Tags("runs"))
	delete(s.OpenAPI.Description().Components.Schemas, "unknown-interface")
}

func (h handlers) status(fuego.ContextNoBody) (Status, error) {
	if h.deps.Store == nil {
		return Status{}, nil
	}
	settings, err := h.deps.Store.Settings()
	if err != nil {
		return Status{}, errors.Wrap(err, "settings")
	}
	counts, err := h.deps.Store.CountDocumentsByState()
	if err != nil {
		return Status{}, errors.Wrap(err, "count documents")
	}
	latest, err := h.deps.Store.LatestSyncRun()
	if err != nil {
		return Status{}, errors.Wrap(err, "latest run")
	}
	out := Status{
		Configured: settings.Configured(),
		Counts:     counts,
		Remarkable: settings.RemarkableConfigured(),
		Running:    h.deps.Engine.Running(),
		Version:    h.deps.Version,
		Zotero:     settings.ZoteroConfigured(),
	}
	if latest != nil {
		run := toRun(*latest)
		out.LastRun = &run
	}
	if next, ok := h.deps.Engine.NextRun(); ok {
		out.NextRunAt = &next
	}
	return out, nil
}

func (h handlers) documents(fuego.ContextNoBody) ([]Document, error) {
	if h.deps.Store == nil {
		return nil, nil
	}
	docs, err := h.deps.Store.ListDocuments()
	if err != nil {
		return nil, errors.Wrap(err, "list documents")
	}
	out := make([]Document, 0, len(docs))
	for _, d := range docs {
		out = append(out, Document{
			AnnotatedKey:    d.AnnotatedKey,
			AnnotationCount: d.AnnotationCount,
			Filename:        d.Filename,
			ID:              d.ID,
			LastError:       d.LastError,
			LastPulledAt:    timePtr(d.LastPulledAt),
			LastPushedAt:    timePtr(d.LastPushedAt),
			RemarkableID:    d.RemarkableID,
			State:           d.State,
			Title:           d.Title,
			UpdatedAt:       d.UpdatedAt.Time,
			ZoteroKey:       d.ZoteroKey,
			ZoteroParentKey: d.ZoteroParentKey,
		})
	}
	return out, nil
}

func (h handlers) runs(fuego.ContextNoBody) ([]Run, error) {
	if h.deps.Store == nil {
		return nil, nil
	}
	runs, err := h.deps.Store.ListSyncRuns(50)
	if err != nil {
		return nil, errors.Wrap(err, "list runs")
	}
	out := make([]Run, 0, len(runs))
	for _, r := range runs {
		out = append(out, toRun(r))
	}
	return out, nil
}

func (h handlers) sync(fuego.ContextNoBody) (SyncResponse, error) {
	if h.deps.Engine == nil {
		return SyncResponse{}, nil
	}
	settings, err := h.deps.Store.Settings()
	if err != nil {
		return SyncResponse{}, errors.Wrap(err, "settings")
	}
	if !settings.Configured() {
		return SyncResponse{Message: "zotero and remarkable must be configured first"}, nil
	}
	if !h.deps.Engine.TriggerAsync(db.TriggerManual) {
		return SyncResponse{Message: "a sync is already running"}, nil
	}
	return SyncResponse{Message: "sync started", Started: true}, nil
}

func toRun(r db.SyncRun) Run {
	return Run{
		Error:      r.Error,
		FinishedAt: timePtr(r.FinishedAt),
		ID:         r.ID,
		Pulled:     r.Pulled,
		Pushed:     r.Pushed,
		Removed:    r.Removed,
		StartedAt:  r.StartedAt.Time,
		Status:     r.Status,
		Trigger:    r.Trigger,
	}
}

func timePtr(t *ts.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	v := t.Time
	return &v
}

func Spec(path string) *openapi3.T {
	s := fuego.NewServer(fuego.WithEngineOptions(fuego.WithOpenAPIConfig(fuego.OpenAPIConfig{
		DisableMessages:  true,
		JSONFilePath:     path,
		PrettyFormatJSON: true,
		Info: &openapi3.Info{
			Description: "REST API for zotrem: sync status, synced documents and manual sync runs.",
			Title:       "zotrem API",
			Version:     "1.0.0",
		},
	})))
	Register(s, Deps{})
	return s.OutputOpenAPISpec()
}
