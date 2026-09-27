package db

import (
	"testing"

	"github.com/kenners/zotrem/pkg/remarkable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func open(t *testing.T) *DB {
	t.Helper()
	d, err := New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { d.Close() })
	return d
}

func TestSettingsRoundTrip(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	d := open(t)
	s, err := d.Settings()
	r.NoError(err)
	a.Equal(DefaultSettings(), s)
	a.False(s.Configured())
	s.ZoteroAPIKey = "abc"
	s.ZoteroLibraryID = 42
	s.RemarkableDeviceToken = "dev"
	s.SyncIntervalMinutes = 5
	s.AnnotatedPDF = false
	r.NoError(d.SaveSettings(s))
	loaded, err := d.Settings()
	r.NoError(err)
	a.Equal(s, loaded)
	a.True(loaded.Configured())
	a.Equal("zotrem:synced", loaded.StateTag(StateSynced))
	r.NoError(d.SetSetting(KeyRemarkableUserToken, "user"))
	loaded, err = d.Settings()
	r.NoError(err)
	a.Equal("user", loaded.RemarkableUserToken)
}

func TestDocumentsAndAnnotations(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	d := open(t)
	doc, err := d.InsertDocument(NewDocument{Filename: "paper.pdf", Title: "Paper", ZoteroKey: "ATT1", ZoteroMD5: "md5", ZoteroParentKey: "PAR1"})
	r.NoError(err)
	a.Equal(StateQueued, doc.State)
	_, err = d.InsertDocument(NewDocument{Filename: "dup.pdf", Title: "Dup", ZoteroKey: "ATT1"})
	a.Error(err)

	doc.RemarkableID = "rm-1"
	doc.RemarkableHash = "h1"
	doc.State = StateSynced
	doc, err = d.UpdateDocument(doc)
	r.NoError(err)
	byKey, err := d.DocumentByZoteroKey("ATT1")
	r.NoError(err)
	a.Equal(doc.ID, byKey.ID)
	byRM, err := d.DocumentByRemarkableID("rm-1")
	r.NoError(err)
	a.Equal(StateSynced, byRM.State)
	_, err = d.DocumentByZoteroKey("nope")
	a.True(IsDocumentNotFound(err))

	r.NoError(d.UpsertAnnotation(Annotation{DocumentID: doc.ID, Kind: "ink", RmKey: "p/1:2", Signature: "sig", ZoteroKey: "ANN1", ZoteroVersion: 3}))
	r.NoError(d.UpsertAnnotation(Annotation{DocumentID: doc.ID, Kind: "ink", RmKey: "p/1:2", Signature: "sig2", ZoteroKey: "ANN1", ZoteroVersion: 4}))
	anns, err := d.ListAnnotations(doc.ID)
	r.NoError(err)
	r.Len(anns, 1)
	a.Equal("sig2", anns[0].Signature)
	a.Equal(4, anns[0].ZoteroVersion)
	r.NoError(d.DeleteAnnotation(doc.ID, "p/1:2"))
	anns, err = d.ListAnnotations(doc.ID)
	r.NoError(err)
	a.Empty(anns)

	counts, err := d.CountDocumentsByState()
	r.NoError(err)
	a.Equal(map[string]int{StateSynced: 1}, counts)
	r.NoError(d.DeleteDocument(doc.ID))
	a.True(IsDocumentNotFound(d.DeleteDocument(doc.ID)))
}

func TestSyncRuns(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	d := open(t)
	latest, err := d.LatestSyncRun()
	r.NoError(err)
	a.Nil(latest)
	run, err := d.StartSyncRun(TriggerManual)
	r.NoError(err)
	a.Equal(RunStatusRunning, run.Status)
	r.NoError(d.AddSyncEvent(run.ID, 0, LevelInfo, "started"))
	r.NoError(d.AddSyncEvent(run.ID, 7, LevelError, "boom"))
	run.Status = RunStatusOK
	run.Pushed = 2
	r.NoError(d.FinishSyncRun(run))
	latest, err = d.LatestSyncRun()
	r.NoError(err)
	r.NotNil(latest)
	a.Equal(RunStatusOK, latest.Status)
	a.Equal(2, latest.Pushed)
	a.NotNil(latest.FinishedAt)
	events, err := d.ListSyncEvents(run.ID)
	r.NoError(err)
	r.Len(events, 2)
	a.Equal(int64(7), events[1].DocumentID)
	docEvents, err := d.ListDocumentEvents(7, 10)
	r.NoError(err)
	a.Len(docEvents, 1)
	runs, err := d.ListSyncRuns(10)
	r.NoError(err)
	a.Len(runs, 1)
}

func TestRemarkableCache(t *testing.T) {
	a := assert.New(t)
	d := open(t)
	_, ok := d.CachedItem("x", "h")
	a.False(ok)
	d.CacheItem(remarkable.Item{Entry: remarkable.Entry{Hash: "h", ID: "x"}, ItemType: remarkable.TypeCollection, Parent: "", VisibleName: "Zotero"})
	item, ok := d.CachedItem("x", "h")
	a.True(ok)
	a.Equal("Zotero", item.VisibleName)
	a.True(item.IsFolder())
	_, ok = d.CachedItem("x", "other")
	a.False(ok)
	a.NoError(d.ClearRemarkableCache())
	_, ok = d.CachedItem("x", "h")
	a.False(ok)
}
