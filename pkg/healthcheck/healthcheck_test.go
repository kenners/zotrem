package healthcheck_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/kenners/zotrem/pkg/healthcheck"
	"github.com/kenners/zotrem/pkg/healthcheck/healthchecktest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		in      string
		name    string
		want    string
		wantErr bool
	}{
		{in: "", name: "empty", want: ""},
		{in: "  https://hc-ping.com/abc/  ", name: "trims space and slash", want: "https://hc-ping.com/abc"},
		{in: "https://hc-ping.com/key/slug?create=1", name: "keeps query", want: "https://hc-ping.com/key/slug?create=1"},
		{in: "http://healthchecks.local/ping/abc", name: "self-hosted http", want: "http://healthchecks.local/ping/abc"},
		{in: "hc-ping.com/abc", name: "no scheme", wantErr: true},
		{in: "ftp://hc-ping.com/abc", name: "wrong scheme", wantErr: true},
		{in: "https:///abc", name: "no host", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := healthcheck.Normalize(tt.in)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSignalURL(t *testing.T) {
	tests := []struct {
		base   string
		name   string
		signal string
		want   string
	}{
		{base: "https://hc-ping.com/abc", name: "success", signal: healthcheck.Success, want: "https://hc-ping.com/abc"},
		{base: "https://hc-ping.com/abc", name: "start", signal: healthcheck.Start, want: "https://hc-ping.com/abc/start"},
		{base: "https://hc-ping.com/key/slug?create=1", name: "fail keeps query", signal: healthcheck.Fail, want: "https://hc-ping.com/key/slug/fail?create=1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := healthcheck.SignalURL(tt.base, tt.signal)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPing(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	hc := healthchecktest.New()
	t.Cleanup(hc.Close)
	c := healthcheck.New(nil)
	r.NoError(c.Ping(context.Background(), hc.CheckURL(), healthcheck.Start, ""))
	r.NoError(c.Ping(context.Background(), hc.CheckURL(), healthcheck.Fail, strings.Repeat("é", healthcheck.MaxBody)))
	pings := hc.Pings()
	r.Len(pings, 2)
	a.Equal(http.MethodPost, pings[0].Method)
	a.Equal("start", pings[0].Signal)
	a.Equal("fail", pings[1].Signal)
	a.LessOrEqual(len(pings[1].Body), healthcheck.MaxBody)
	a.True(strings.HasPrefix(pings[1].Body, "éé"))

	hc.SetStatus(http.StatusNotFound)
	a.Error(c.Ping(context.Background(), hc.CheckURL(), healthcheck.Success, ""))
}
