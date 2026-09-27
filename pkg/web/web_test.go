package web_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/go-fuego/fuego"
	"github.com/kenners/zotrem/pkg/api"
	"github.com/kenners/zotrem/pkg/db"
	"github.com/kenners/zotrem/pkg/remarkable/remarkabletest"
	"github.com/kenners/zotrem/pkg/sync"
	"github.com/kenners/zotrem/pkg/web"
	"github.com/kenners/zotrem/pkg/zotero/zoterotest"
	"github.com/kenners/zotrem/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type harness struct {
	*testkit.HTML
	engine *sync.Engine
	rm     *remarkabletest.Server
	store  *db.DB
	zot    *zoterotest.Server
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	store, err := db.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	zot := zoterotest.New()
	t.Cleanup(zot.Close)
	rmSrv := remarkabletest.New()
	t.Cleanup(rmSrv.Close)
	engine := sync.New(store, nil, sync.Options{
		RemarkableAuthHost: rmSrv.URL,
		RemarkableSyncHost: rmSrv.URL,
		Sleep:              func(time.Duration) {},
		ZoteroBaseURL:      zot.URL,
	})
	srv := fuego.NewServer(fuego.WithEngineOptions(fuego.WithOpenAPIConfig(fuego.OpenAPIConfig{DisableLocalSave: true, DisableMessages: true, Info: &openapi3.Info{Title: "test", Version: "0"}})))
	api.Register(srv, api.Deps{Engine: engine, Store: store, Version: "test"})
	srv.Mux.Handle("/", web.New(store, engine, nil).Handler())
	return &harness{HTML: testkit.NewHTML(t, srv.Mux), engine: engine, rm: rmSrv, store: store, zot: zot}
}

func TestDashboardUnconfigured(t *testing.T) {
	a := assert.New(t)
	h := newHarness(t)
	h.Load("/")
	a.Equal(http.StatusOK, h.Status)
	h.ElementTextContains("h1", "Dashboard")
	h.ElementTextContains(".z-card", "Welcome to zotrem")
	h.ElementPresent(`button[disabled]`)
	h.ElementTextContains(".gm-label.active", "Dashboard")
}

func TestSettingsFlow(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	h := newHarness(t)
	h.Load("/settings")
	h.ElementPresent(`input[name="zotero_api_key"]`)
	h.ElementPresent(`input[name="code"]`)

	resp, err := h.Client.PostForm(h.Server.URL+"/settings/zotero", map[string][]string{"zotero_api_key": {"wrong"}})
	r.NoError(err)
	resp.Body.Close()
	a.Equal(http.StatusOK, resp.StatusCode)
	settings, err := h.store.Settings()
	r.NoError(err)
	a.Empty(settings.ZoteroAPIKey)

	resp, err = h.Client.PostForm(h.Server.URL+"/settings/zotero", map[string][]string{"zotero_api_key": {h.zot.APIKey}})
	r.NoError(err)
	resp.Body.Close()
	settings, err = h.store.Settings()
	r.NoError(err)
	a.Equal(h.zot.APIKey, settings.ZoteroAPIKey)
	a.Equal(h.zot.UserID, settings.ZoteroLibraryID)
	a.Equal("tester", settings.ZoteroUsername)

	resp, err = h.Client.PostForm(h.Server.URL+"/settings/remarkable/pair", map[string][]string{"code": {"nope1234"}})
	r.NoError(err)
	resp.Body.Close()
	settings, err = h.store.Settings()
	r.NoError(err)
	a.Empty(settings.RemarkableDeviceToken)

	resp, err = h.Client.PostForm(h.Server.URL+"/settings/remarkable/pair", map[string][]string{"code": {h.rm.PairingCode}})
	r.NoError(err)
	resp.Body.Close()
	settings, err = h.store.Settings()
	r.NoError(err)
	a.Equal(h.rm.DeviceToken, settings.RemarkableDeviceToken)
	a.True(settings.Configured())

	resp, err = h.Client.PostForm(h.Server.URL+"/settings", map[string][]string{
		"tag_send":              {"to-tablet"},
		"tag_prefix":            {"rm:"},
		"remarkable_folder":     {"Papers"},
		"sync_interval_minutes": {"30"},
		"annotated_pdf":         {"true"},
	})
	r.NoError(err)
	resp.Body.Close()
	settings, err = h.store.Settings()
	r.NoError(err)
	a.Equal("to-tablet", settings.TagSend)
	a.Equal("rm:", settings.TagPrefix)
	a.Equal("Papers", settings.RemarkableFolder)
	a.Equal(30, settings.SyncIntervalMinutes)
	a.True(settings.AnnotatedPDF)
	a.False(settings.WriteAnnotations)

	h.Load("/settings")
	h.ElementTextContains("#zotero", "Connected as tester")
	h.ElementTextContains("#remarkable", "paired")
	h.ElementAttributeEquals(`input[name="tag_send"]`, "value", "to-tablet")
}

func TestSyncAndPages(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	h := newHarness(t)
	settings := db.DefaultSettings()
	settings.ZoteroAPIKey = h.zot.APIKey
	settings.ZoteroLibraryID = h.zot.UserID
	settings.RemarkableDeviceToken = h.rm.DeviceToken
	r.NoError(h.store.SaveSettings(settings))
	paper := h.zot.AddPaper("A paper about sepsis", "send-to-remarkable")
	h.zot.AddPDF(paper.Key, "sepsis.pdf", []byte("%PDF-1.4\n"))

	resp, err := h.Client.Post(h.Server.URL+"/api/sync", "application/json", nil)
	r.NoError(err)
	var started api.SyncResponse
	r.NoError(json.NewDecoder(resp.Body).Decode(&started))
	resp.Body.Close()
	a.True(started.Started)
	r.Eventually(func() bool {
		latest, _ := h.store.LatestSyncRun()
		return latest != nil && latest.Status != db.RunStatusRunning && !h.engine.Running()
	}, 10*time.Second, 20*time.Millisecond)

	h.Load("/")
	h.ElementTextContains(".z-table", "A paper about sepsis")
	h.ElementTextContains(".z-badge-synced", "On reMarkable")
	h.Load("/documents?state=synced")
	h.ElementTextContains(".z-table", "sepsis.pdf")
	h.Load("/documents?state=error")
	h.ElementTextContains(".z-card", "No documents yet")
	h.Load("/documents/1")
	h.ElementTextContains("h2", "A paper about sepsis")
	h.ElementTextContains(".z-kv", paper.Key)
	h.Load("/runs")
	h.ElementTextContains(".z-table", "1 sent")
	h.Load("/runs/1")
	h.ElementTextContains(".z-events", "sent")
	h.Load("/status")
	h.ElementPresent("#sync-status")

	resp, err = h.Client.Get(h.Server.URL + "/api/status")
	r.NoError(err)
	var status api.Status
	r.NoError(json.NewDecoder(resp.Body).Decode(&status))
	resp.Body.Close()
	a.True(status.Configured)
	a.Equal(1, status.Counts[db.StateSynced])
	r.NotNil(status.LastRun)
	a.Equal(1, status.LastRun.Pushed)

	resp, err = h.Client.Get(h.Server.URL + "/api/documents")
	r.NoError(err)
	var docs []api.Document
	r.NoError(json.NewDecoder(resp.Body).Decode(&docs))
	resp.Body.Close()
	r.Len(docs, 1)
	a.Equal("synced", docs[0].State)

	resp, err = h.Client.Get(h.Server.URL + "/documents/999")
	r.NoError(err)
	resp.Body.Close()
	a.Equal(http.StatusNotFound, resp.StatusCode)
}

func TestBasicAuth(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	store, err := db.New(":memory:")
	r.NoError(err)
	t.Cleanup(func() { store.Close() })
	engine := sync.New(store, nil, sync.Options{})
	srv := web.New(store, engine, nil)
	srv.SetPassword("secret")
	h := testkit.NewHTML(t, srv.Handler())
	resp, err := h.Client.Get(h.Server.URL + "/")
	r.NoError(err)
	resp.Body.Close()
	a.Equal(http.StatusUnauthorized, resp.StatusCode)
	req, _ := http.NewRequest(http.MethodGet, h.Server.URL+"/", nil)
	req.SetBasicAuth("zotrem", "secret")
	resp, err = h.Client.Do(req)
	r.NoError(err)
	resp.Body.Close()
	a.Equal(http.StatusOK, resp.StatusCode)
}
