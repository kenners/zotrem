package web

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/cockroachdb/errors"
	"github.com/kenners/zotrem/pkg/db"
	"github.com/kenners/zotrem/pkg/healthcheck"
	"github.com/kenners/zotrem/pkg/remarkable"
	"github.com/kenners/zotrem/pkg/server/httperr"
	"github.com/kenners/zotrem/pkg/server/logging"
	"github.com/kenners/zotrem/pkg/sync"
	"github.com/kenners/zotrem/pkg/ui"
)

type Server struct {
	engine *sync.Engine
	log    *slog.Logger
	store  *db.DB
}

func New(store *db.DB, engine *sync.Engine, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	return &Server{engine: engine, log: log, store: store}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleDashboard)
	mux.HandleFunc("GET /documents", s.handleDocuments)
	mux.HandleFunc("GET /documents/{id}", s.handleDocument)
	mux.HandleFunc("GET /manifest.webmanifest", s.handleManifest)
	mux.HandleFunc("GET /runs", s.handleRuns)
	mux.HandleFunc("GET /runs/table", s.handleRunsTable)
	mux.HandleFunc("GET /runs/{id}", s.handleRun)
	mux.HandleFunc("GET /runs/{id}/detail", s.handleRunDetail)
	mux.HandleFunc("GET /settings", s.handleSettings)
	mux.HandleFunc("POST /settings", s.handleSaveSettings)
	mux.HandleFunc("POST /settings/healthcheck", s.handleSaveHealthcheck)
	mux.HandleFunc("POST /settings/healthcheck/test", s.handleTestHealthcheck)
	mux.HandleFunc("POST /settings/remarkable/pair", s.handlePair)
	mux.HandleFunc("POST /settings/remarkable/unpair", s.handleUnpair)
	mux.HandleFunc("POST /settings/zotero", s.handleSaveZotero)
	mux.HandleFunc("GET /status", s.handleStatus)
	mux.HandleFunc("POST /sync", s.handleSync)
	mux.Handle("GET /static/", http.StripPrefix("/static/", ui.StaticHandler()))
	return logging.Middleware(s.log, mux)
}

func (s *Server) Status() (ui.Status, error) {
	settings, err := s.store.Settings()
	if err != nil {
		return ui.Status{}, err
	}
	counts, err := s.store.CountDocumentsByState()
	if err != nil {
		return ui.Status{}, err
	}
	latest, err := s.store.LatestSyncRun()
	if err != nil {
		return ui.Status{}, err
	}
	status := ui.Status{
		Configured:           settings.Configured(),
		Counts:               counts,
		Folder:               settings.RemarkableFolder,
		LastRun:              latest,
		NextRun:              "manual",
		RemarkableConfigured: settings.RemarkableConfigured(),
		Running:              s.engine.Running(),
		ZoteroConfigured:     settings.ZoteroConfigured(),
		ZoteroUsername:       settings.ZoteroUsername,
	}
	if next, ok := s.engine.NextRun(); ok {
		if next.Before(time.Now()) {
			status.NextRun = "soon"
		} else {
			status.NextRun = next.Local().Format("15:04")
		}
	}
	return status, nil
}

func (s *Server) page(active, title string) (ui.PageProps, error) {
	status, err := s.Status()
	if err != nil {
		return ui.PageProps{}, err
	}
	return ui.PageProps{
		Active: active,
		Counts: ui.NavCounts{
			Documents: ui.TotalDocuments(status.Counts),
			Errors:    status.Counts[db.StateError],
		},
		Status: status,
		Title:  title,
	}, nil
}

func (s *Server) rows(docs []db.Document) []ui.DocumentRow {
	settings, _ := s.store.Settings()
	zc := s.engine.ZoteroClient(settings)
	out := make([]ui.DocumentRow, 0, len(docs))
	for _, doc := range docs {
		row := ui.DocumentRow{Document: doc}
		key := doc.ZoteroParentKey
		if key == "" {
			key = doc.ZoteroKey
		}
		row.ZoteroURL = zc.ItemURL(key)
		if doc.AnnotatedKey != "" {
			row.AnnotatedURL = zc.ItemURL(doc.AnnotatedKey)
		}
		out = append(out, row)
	}
	return out
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	page, err := s.page("dashboard", "Dashboard")
	if err != nil {
		s.fail(w, err)
		return
	}
	docs, err := s.store.ListDocuments()
	if err != nil {
		s.fail(w, err)
		return
	}
	if len(docs) > 8 {
		docs = docs[:8]
	}
	var events []db.SyncEvent
	if page.Status.LastRun != nil {
		events, err = s.store.ListSyncEvents(page.Status.LastRun.ID)
		if err != nil {
			s.fail(w, err)
			return
		}
	}
	s.render(w, r, ui.Dashboard(ui.DashboardProps{Events: events, Page: page, Recent: s.rows(docs)}))
}

func (s *Server) handleDocuments(w http.ResponseWriter, r *http.Request) {
	page, err := s.page("documents", "Documents")
	if err != nil {
		s.fail(w, err)
		return
	}
	docs, err := s.store.ListDocuments()
	if err != nil {
		s.fail(w, err)
		return
	}
	filter := r.URL.Query().Get("state")
	if filter == "" {
		filter = "all"
	}
	filtered := make([]db.Document, 0, len(docs))
	for _, doc := range docs {
		switch {
		case filter == "all" && doc.State != db.StateRemoved:
			filtered = append(filtered, doc)
		case filter == doc.State:
			filtered = append(filtered, doc)
		}
	}
	s.render(w, r, ui.Documents(ui.DocumentsProps{Documents: s.rows(filtered), Filter: filter, Page: page}))
}

func (s *Server) handleDocument(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	doc, err := s.store.Document(id)
	if db.IsDocumentNotFound(err) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	page, err := s.page("documents", doc.Title)
	if err != nil {
		s.fail(w, err)
		return
	}
	events, err := s.store.ListDocumentEvents(id, 50)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, ui.Document(ui.DocumentProps{Document: s.rows([]db.Document{doc})[0], Events: events, Page: page}))
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	page, err := s.page("runs", "Sync log")
	if err != nil {
		s.fail(w, err)
		return
	}
	runs, err := s.store.ListSyncRuns(100)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, ui.Runs(ui.RunsProps{Page: page, Runs: runs}))
}

func (s *Server) handleRunsTable(w http.ResponseWriter, r *http.Request) {
	runs, err := s.store.ListSyncRuns(100)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, ui.RunsTable(runs))
}

func (s *Server) runProps(w http.ResponseWriter, r *http.Request) (ui.RunProps, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return ui.RunProps{}, false
	}
	run, err := s.store.SyncRun(id)
	if errors.Is(err, db.ErrRunNotFound) {
		http.Error(w, "not found", http.StatusNotFound)
		return ui.RunProps{}, false
	}
	if err != nil {
		s.fail(w, err)
		return ui.RunProps{}, false
	}
	events, err := s.store.ListSyncEvents(id)
	if err != nil {
		s.fail(w, err)
		return ui.RunProps{}, false
	}
	return ui.RunProps{Events: events, Run: run}, true
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	props, ok := s.runProps(w, r)
	if !ok {
		return
	}
	page, err := s.page("runs", "Sync "+ui.WhenTS(props.Run))
	if err != nil {
		s.fail(w, err)
		return
	}
	props.Page = page
	s.render(w, r, ui.Run(props))
}

func (s *Server) handleRunDetail(w http.ResponseWriter, r *http.Request) {
	props, ok := s.runProps(w, r)
	if !ok {
		return
	}
	s.render(w, r, ui.RunDetail(props))
}

func (s *Server) settingsPage(w http.ResponseWriter, r *http.Request, flash, errMsg string) {
	page, err := s.page("settings", "Settings")
	if err != nil {
		s.fail(w, err)
		return
	}
	settings, err := s.store.Settings()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, ui.SettingsPage(ui.SettingsProps{
		Error:      errMsg,
		Flash:      flash,
		Page:       page,
		PairingURL: remarkable.PairingURL,
		Settings:   settings,
	}))
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	s.settingsPage(w, r, q.Get("flash"), q.Get("error"))
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	settings, err := s.store.Settings()
	if err != nil {
		s.fail(w, err)
		return
	}
	form := func(name string) string { return strings.TrimSpace(r.FormValue(name)) }
	if tag := form("tag_send"); tag != "" {
		settings.TagSend = tag
	}
	settings.TagPrefix = form("tag_prefix")
	if folder := form("remarkable_folder"); folder != "" {
		settings.RemarkableFolder = folder
	}
	if interval, err := strconv.Atoi(form("sync_interval_minutes")); err == nil && interval >= 0 {
		settings.SyncIntervalMinutes = interval
	}
	settings.AnnotatedPDF = r.FormValue("annotated_pdf") == "true"
	settings.TrashWhenUntagged = r.FormValue("trash_when_untagged") == "true"
	settings.UntagWhenDeleted = r.FormValue("untag_when_deleted") == "true"
	settings.WriteAnnotations = r.FormValue("write_annotations") == "true"
	if err := s.store.SaveSettings(settings); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/settings?flash=Settings+saved", http.StatusSeeOther)
}

func (s *Server) handleSaveHealthcheck(w http.ResponseWriter, r *http.Request) {
	target, ok := s.saveHealthcheck(w, r)
	if !ok {
		return
	}
	flash := "Healthcheck+ping+URL+saved"
	if target == "" {
		flash = "Healthcheck+pings+turned+off"
	}
	http.Redirect(w, r, "/settings?flash="+flash+"#monitoring", http.StatusSeeOther)
}

func (s *Server) handleTestHealthcheck(w http.ResponseWriter, r *http.Request) {
	target, ok := s.saveHealthcheck(w, r)
	if !ok {
		return
	}
	if target == "" {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape("Enter a healthcheck ping URL first")+"#monitoring", http.StatusSeeOther)
		return
	}
	if err := s.engine.PingHealthcheck(r.Context(), target, healthcheck.Log, "zotrem test ping"); err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape("Test ping failed: "+err.Error())+"#monitoring", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/settings?flash="+url.QueryEscape("Saved; test ping sent (it appears as a log entry and does not change the check's status)")+"#monitoring", http.StatusSeeOther)
}

func (s *Server) saveHealthcheck(w http.ResponseWriter, r *http.Request) (string, bool) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return "", false
	}
	target, err := healthcheck.Normalize(r.FormValue("healthcheck_url"))
	if err != nil {
		http.Redirect(w, r, "/settings?error="+url.QueryEscape(err.Error())+"#monitoring", http.StatusSeeOther)
		return "", false
	}
	if err := s.store.SetSetting(db.KeyHealthcheckURL, target); err != nil {
		s.fail(w, err)
		return "", false
	}
	return target, true
}

func (s *Server) handleSaveZotero(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	settings, err := s.store.Settings()
	if err != nil {
		s.fail(w, err)
		return
	}
	apiKey := strings.TrimSpace(r.FormValue("zotero_api_key"))
	if apiKey == "" {
		s.settingsPage(w, r, "", "Enter a Zotero API key.")
		return
	}
	probe := settings
	probe.ZoteroAPIKey = apiKey
	probe.ZoteroLibraryType = "user"
	probe.ZoteroLibraryID = 0
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	info, err := s.engine.ZoteroClient(probe).CurrentKey(ctx)
	if err != nil {
		s.settingsPage(w, r, "", "Zotero rejected the API key: "+err.Error())
		return
	}
	settings.ZoteroAPIKey = apiKey
	settings.ZoteroUsername = info.Username
	if r.FormValue("zotero_library_type") == "group" {
		groupID, err := strconv.ParseInt(strings.TrimSpace(r.FormValue("zotero_group_id")), 10, 64)
		if err != nil || groupID <= 0 {
			s.settingsPage(w, r, "", "Enter the numeric group ID for a group library.")
			return
		}
		settings.ZoteroLibraryType = "group"
		settings.ZoteroLibraryID = groupID
	} else {
		settings.ZoteroLibraryType = "user"
		settings.ZoteroLibraryID = info.UserID
	}
	if !info.Access.User.Write && settings.ZoteroLibraryType == "user" {
		s.settingsPage(w, r, "", "The API key has no write access to your library. Create a key with write access.")
		return
	}
	if err := s.store.SaveSettings(settings); err != nil {
		s.fail(w, err)
		return
	}
	http.Redirect(w, r, "/settings?flash=Connected+to+Zotero+as+"+info.Username, http.StatusSeeOther)
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.fail(w, err)
		return
	}
	code := strings.TrimSpace(r.FormValue("code"))
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	settings, err := s.store.Settings()
	if err != nil {
		s.fail(w, err)
		return
	}
	token, err := s.engine.Register(ctx, code)
	if err != nil {
		s.settingsPage(w, r, "", "Pairing failed: "+err.Error())
		return
	}
	settings.RemarkableDeviceToken = token
	settings.RemarkableUserToken = ""
	if err := s.store.SaveSettings(settings); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.store.ClearRemarkableCache(); err != nil {
		s.log.Warn("clear remarkable cache", "error", err)
	}
	http.Redirect(w, r, "/settings?flash=Paired+with+the+reMarkable+cloud", http.StatusSeeOther)
}

func (s *Server) handleUnpair(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings()
	if err != nil {
		s.fail(w, err)
		return
	}
	settings.RemarkableDeviceToken = ""
	settings.RemarkableUserToken = ""
	if err := s.store.SaveSettings(settings); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.store.ClearRemarkableCache(); err != nil {
		s.log.Warn("clear remarkable cache", "error", err)
	}
	http.Redirect(w, r, "/settings?flash=Unpaired", http.StatusSeeOther)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.Status()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.render(w, r, ui.TopbarActions(status))
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.Settings()
	if err != nil {
		s.fail(w, err)
		return
	}
	if !settings.Configured() {
		http.Redirect(w, r, "/settings?error=Configure+Zotero+and+pair+the+reMarkable+first", http.StatusSeeOther)
		return
	}
	s.engine.TriggerAsync(db.TriggerManual)
	back := r.Header.Get("Referer")
	if back == "" || strings.Contains(back, "/status") {
		back = "/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (s *Server) handleManifest(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/manifest+json")
	json.NewEncoder(w).Encode(map[string]any{
		"background_color": "#ffffff",
		"display":          "standalone",
		"icons": []map[string]string{
			{"sizes": "192x192", "src": "/static/icon-192.png", "type": "image/png"},
			{"sizes": "512x512", "src": "/static/icon-512.png", "type": "image/png"},
		},
		"name":        "zotrem",
		"short_name":  "zotrem",
		"start_url":   "/",
		"theme_color": "#0f172a",
	})
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	httperr.Log(s.log, "web error", err)
	httperr.Error(w, err, http.StatusInternalServerError)
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, comp templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := comp.Render(r.Context(), w); err != nil {
		httperr.Error(w, err, http.StatusInternalServerError)
	}
}
