package web_test

import (
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/kenners/zotrem/pkg/db"
	"github.com/kenners/zotrem/pkg/healthcheck/healthchecktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunsUpdateLive(t *testing.T) {
	r := require.New(t)
	h := newHarness(t)
	h.Load("/runs")
	h.ElementAttributeEquals("#runs-table", "hx-trigger", "every 30s")
	h.ElementTextContains("#runs-table", "No sync has run yet")

	run, err := h.store.StartSyncRun(db.TriggerManual)
	r.NoError(err)
	h.Load("/runs")
	h.ElementAttributeEquals("#runs-table", "hx-get", "/runs/table")
	h.ElementAttributeEquals("#runs-table", "hx-trigger", "every 3s")
	h.ElementTextContains("#runs-table .z-badge", "Running")
	h.Load("/runs/1")
	h.ElementAttributeEquals("#run-detail", "hx-get", "/runs/1/detail")
	h.ElementAttributeEquals("#run-detail", "hx-trigger", "every 3s")

	run.Status = db.RunStatusOK
	run.Pushed = 2
	r.NoError(h.store.FinishSyncRun(run))
	h.Load("/runs/table")
	h.ElementAttributeEquals("#runs-table", "hx-trigger", "every 30s")
	h.ElementTextContains("#runs-table .z-badge", "OK")
	h.ElementTextContains("#runs-table", "2 sent")
	h.Load("/runs/1/detail")
	h.ElementTextContains("#run-detail .z-badge", "OK")
	h.ElementAbsent("#run-detail[hx-get]")
}

func TestHealthcheckSettings(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	h := newHarness(t)
	hc := healthchecktest.New()
	t.Cleanup(hc.Close)
	post := func(path, value string) string {
		resp, err := h.Client.PostForm(h.Server.URL+path, url.Values{"healthcheck_url": {value}})
		r.NoError(err)
		defer resp.Body.Close()
		r.Equal(http.StatusOK, resp.StatusCode)
		body, err := io.ReadAll(resp.Body)
		r.NoError(err)
		return string(body)
	}
	stored := func() string {
		settings, err := h.store.Settings()
		r.NoError(err)
		return settings.HealthcheckURL
	}

	a.Contains(post("/settings/healthcheck", "not a url"), "is not an http or https URL")
	a.Empty(stored())

	a.Contains(post("/settings/healthcheck", hc.CheckURL()+"/"), "Healthcheck ping URL saved")
	a.Equal(hc.CheckURL(), stored())
	h.Load("/settings")
	h.ElementAttributeEquals(`input[name="healthcheck_url"]`, "value", hc.CheckURL())

	a.Contains(post("/settings/healthcheck/test", hc.CheckURL()), "test ping sent")
	a.Equal([]string{"log"}, hc.Signals())

	hc.SetStatus(http.StatusNotFound)
	a.Contains(post("/settings/healthcheck/test", hc.CheckURL()), "Test ping failed")

	a.Contains(post("/settings/healthcheck/test", ""), "Enter a healthcheck ping URL first")
	a.Contains(post("/settings/healthcheck", ""), "Healthcheck pings turned off")
	a.Empty(stored())
}
