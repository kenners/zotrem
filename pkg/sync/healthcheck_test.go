package sync

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kenners/zotrem/pkg/db"
	"github.com/kenners/zotrem/pkg/healthcheck/healthchecktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthcheckPings(t *testing.T) {
	tests := []struct {
		name        string
		noURL       bool
		setup       func(f *fixture, s *db.Settings)
		status      int
		wantBody    string
		wantRunErr  bool
		wantSignals []string
		wantWarn    bool
	}{
		{
			name: "success",
			setup: func(f *fixture, s *db.Settings) {
				paper := f.zot.AddPaper("Good", "send-to-remarkable")
				f.zot.AddPDF(paper.Key, "good.pdf", f.pdf)
			},
			wantBody:    "1 sent, 0 pulled, 0 removed",
			wantSignals: []string{"start", ""},
		},
		{
			name: "document error",
			setup: func(f *fixture, s *db.Settings) {
				paper := f.zot.AddPaper("Linked", "send-to-remarkable")
				f.zot.AddLinkedPDF(paper.Key, "attachments:linked.pdf")
			},
			wantBody:    "linked file",
			wantSignals: []string{"start", "fail"},
		},
		{
			name:        "run error",
			setup:       func(f *fixture, s *db.Settings) { s.ZoteroAPIKey = "" },
			wantBody:    "zotero is not configured",
			wantRunErr:  true,
			wantSignals: []string{"start", "fail"},
		},
		{
			name:        "ping rejected",
			status:      http.StatusNotFound,
			wantSignals: []string{"start", ""},
			wantWarn:    true,
		},
		{
			name:  "not configured",
			noURL: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			r := require.New(t)
			f := newFixture(t)
			hc := healthchecktest.New()
			t.Cleanup(hc.Close)
			if tt.status != 0 {
				hc.SetStatus(tt.status)
			}
			settings, err := f.store.Settings()
			r.NoError(err)
			if !tt.noURL {
				settings.HealthcheckURL = hc.CheckURL()
			}
			if tt.setup != nil {
				tt.setup(f, &settings)
			}
			r.NoError(f.store.SaveSettings(settings))

			run, err := f.engine.Run(context.Background(), db.TriggerManual)
			if tt.wantRunErr {
				r.Error(err)
			} else {
				r.NoError(err)
			}
			a.Equal(tt.wantSignals, hc.Signals())
			if tt.wantBody != "" {
				pings := hc.Pings()
				r.NotEmpty(pings)
				a.Contains(pings[len(pings)-1].Body, tt.wantBody)
			}
			events, err := f.store.ListSyncEvents(run.ID)
			r.NoError(err)
			warned := false
			for _, e := range events {
				if e.Level == db.LevelWarn && strings.Contains(e.Message, "ping healthchecks.io") {
					warned = true
				}
			}
			a.Equal(tt.wantWarn, warned)
		})
	}
}
