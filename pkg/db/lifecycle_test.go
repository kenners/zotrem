package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInterruptSyncRuns(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	d := open(t)
	done, err := d.StartSyncRun(TriggerManual)
	r.NoError(err)
	done.Status = RunStatusOK
	r.NoError(d.FinishSyncRun(done))
	stuck, err := d.StartSyncRun(TriggerScheduled)
	r.NoError(err)

	n, err := d.InterruptSyncRuns("interrupted")
	r.NoError(err)
	a.EqualValues(1, n)
	got, err := d.SyncRun(stuck.ID)
	r.NoError(err)
	a.Equal(RunStatusError, got.Status)
	a.Equal("interrupted", got.Error)
	a.NotNil(got.FinishedAt)
	got, err = d.SyncRun(done.ID)
	r.NoError(err)
	a.Equal(RunStatusOK, got.Status)
	a.Empty(got.Error)

	n, err = d.InterruptSyncRuns("interrupted")
	r.NoError(err)
	a.Zero(n)
}

func TestDatabaseFilePermissions(t *testing.T) {
	tests := []struct {
		existing bool
		name     string
	}{
		{name: "new data directory"},
		{existing: true, name: "existing world-readable database"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			r := require.New(t)
			dir := filepath.Join(t.TempDir(), "data")
			path := filepath.Join(dir, "zotrem.db")
			if tt.existing {
				r.NoError(os.MkdirAll(dir, 0o755))
				r.NoError(os.WriteFile(path, nil, 0o644))
			}
			d, err := New(path)
			r.NoError(err)
			t.Cleanup(func() { d.Close() })
			r.NoError(d.SetSetting(KeyTagSend, "x"))

			if !tt.existing {
				info, err := os.Stat(dir)
				r.NoError(err)
				a.Equal(os.FileMode(0o700), info.Mode().Perm())
			}
			for _, name := range []string{path, path + "-wal"} {
				info, err := os.Stat(name)
				r.NoError(err)
				a.Equal(os.FileMode(0o600), info.Mode().Perm(), name)
			}
		})
	}
}
