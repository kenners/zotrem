package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"codeberg.org/go-pdf/fpdf"
	"github.com/kenners/zotrem/pkg/db"
	"github.com/kenners/zotrem/pkg/remarkable"
	"github.com/kenners/zotrem/pkg/remarkable/remarkabletest"
	"github.com/kenners/zotrem/pkg/render"
	"github.com/kenners/zotrem/pkg/zotero"
	"github.com/kenners/zotrem/pkg/zotero/zoterotest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fixture struct {
	engine *Engine
	pdf    []byte
	rm     *remarkabletest.Server
	store  *db.DB
	zot    *zoterotest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	store, err := db.New(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	zot := zoterotest.New()
	t.Cleanup(zot.Close)
	rmSrv := remarkabletest.New()
	t.Cleanup(rmSrv.Close)
	settings := db.DefaultSettings()
	settings.ZoteroAPIKey = zot.APIKey
	settings.ZoteroLibraryID = zot.UserID
	settings.RemarkableDeviceToken = rmSrv.DeviceToken
	require.NoError(t, store.SaveSettings(settings))
	engine := New(store, nil, Options{
		RemarkableAuthHost: rmSrv.URL,
		RemarkableSyncHost: rmSrv.URL,
		Sleep:              func(time.Duration) {},
		ZoteroBaseURL:      zot.URL,
	})
	pdf, err := render.BlankPDF([]fpdf.SizeType{{Wd: 612, Ht: 792}, {Wd: 612, Ht: 792}})
	require.NoError(t, err)
	return &fixture{engine: engine, pdf: pdf, rm: rmSrv, store: store, zot: zot}
}

func (f *fixture) run(t *testing.T) db.SyncRun {
	t.Helper()
	run, err := f.engine.Run(context.Background(), db.TriggerManual)
	require.NoError(t, err)
	require.Equal(t, db.RunStatusOK, run.Status, run.Error)
	return run
}

func loadRM(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "rm", "testdata", name))
	require.NoError(t, err)
	return data
}

func (f *fixture) folderChildren(t *testing.T) []remarkabletest.ItemState {
	t.Helper()
	for _, item := range f.rm.Items() {
		if item.Metadata.Type == remarkable.TypeCollection && item.Metadata.VisibleName == "Zotero" {
			return f.rm.ChildrenOf(item.ID)
		}
	}
	return nil
}

func TestRunRequiresConfiguration(t *testing.T) {
	f := newFixture(t)
	require.NoError(t, f.store.SaveSettings(db.DefaultSettings()))
	run, err := f.engine.Run(context.Background(), db.TriggerManual)
	assert.Error(t, err)
	assert.Equal(t, db.RunStatusError, run.Status)
}

func TestPushTaggedPapers(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	f := newFixture(t)
	paper := f.zot.AddPaper("Sepsis in 2026", "send-to-remarkable")
	att := f.zot.AddPDF(paper.Key, "sepsis.pdf", f.pdf)
	f.zot.AddPaper("Untagged paper")
	standalone := f.zot.AddPDF("", "loose.pdf", f.pdf)
	f.zot.SetTags(standalone.Key, "send-to-remarkable")

	run := f.run(t)
	a.Equal(2, run.Pushed)
	children := f.folderChildren(t)
	r.Len(children, 2)
	names := []string{children[0].Metadata.VisibleName, children[1].Metadata.VisibleName}
	a.ElementsMatch([]string{"Sepsis in 2026", "loose.pdf"}, names)
	a.Equal(f.pdf, children[0].Files[children[0].ID+".pdf"])
	a.Equal([]string{"send-to-remarkable", "zotrem:synced"}, f.zot.Tags(paper.Key))
	a.Equal([]string{"send-to-remarkable", "zotrem:synced"}, f.zot.Tags(standalone.Key))
	doc, err := f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	a.Equal(db.StateSynced, doc.State)
	a.Equal(paper.Key, doc.ZoteroParentKey)
	a.NotEmpty(doc.RemarkableID)

	run = f.run(t)
	a.Equal(0, run.Pushed)
	a.Len(f.folderChildren(t), 2)
}

func TestPullAnnotations(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	f := newFixture(t)
	paper := f.zot.AddPaper("Annotated paper", "send-to-remarkable")
	att := f.zot.AddPDF(paper.Key, "paper.pdf", f.pdf)
	f.run(t)
	doc, err := f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)

	f.rm.SetPageStrokes(doc.RemarkableID, 1, loadRM(t, "Color_and_tool_v3.14.4.rm"))
	run := f.run(t)
	a.Equal(1, run.Pulled)
	doc, err = f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	a.Equal(db.StateAnnotated, doc.State)
	a.Equal(31, doc.AnnotationCount)
	a.NotEmpty(doc.AnnotatedKey)
	a.Equal([]string{"send-to-remarkable", "zotrem:annotated", "zotrem:synced"}, f.zot.Tags(paper.Key))

	zoteroAnnotations := f.zot.ChildrenOf(att.Key)
	r.Len(zoteroAnnotations, 31)
	kinds := map[string]int{}
	for _, ann := range zoteroAnnotations {
		kinds[ann.Data["annotationType"].(string)]++
		a.Equal("2", ann.Data["annotationPageLabel"])
	}
	a.Equal(map[string]int{"highlight": 6, "ink": 25}, kinds)
	annotated := f.zot.Item(doc.AnnotatedKey)
	r.NotNil(annotated)
	a.Equal(paper.Key, annotated.Data["parentItem"])
	a.Equal("paper (reMarkable).pdf", annotated.Data["filename"])
	a.NotEmpty(f.zot.Files[doc.AnnotatedKey])
	a.NotEqual(f.pdf, f.zot.Files[doc.AnnotatedKey])
	firstMD5 := doc.AnnotatedMD5
	rows, err := f.store.ListAnnotations(doc.ID)
	r.NoError(err)
	a.Len(rows, 31)

	f.rm.Touch(doc.RemarkableID)
	requestsBefore := len(f.zot.Requests)
	run = f.run(t)
	a.Equal(0, run.Pulled)
	a.Len(f.zot.ChildrenOf(att.Key), 31)
	a.Less(len(f.zot.Requests)-requestsBefore, 6, "an unchanged scene must not write to zotero")

	f.rm.SetPageStrokes(doc.RemarkableID, 1, loadRM(t, "rm2pdf_version6.rm"))
	run = f.run(t)
	a.Equal(1, run.Pulled)
	doc, err = f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	a.Equal(26, doc.AnnotationCount)
	a.Len(f.zot.ChildrenOf(att.Key), 26)
	a.NotEqual(firstMD5, doc.AnnotatedMD5)
	a.Len(f.zot.ChildrenOf(paper.Key), 2)
}

func TestUntagMovesToTrash(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	f := newFixture(t)
	paper := f.zot.AddPaper("Paper", "send-to-remarkable", "clinical")
	att := f.zot.AddPDF(paper.Key, "paper.pdf", f.pdf)
	f.run(t)
	doc, err := f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	f.zot.SetTags(paper.Key, "clinical")
	run := f.run(t)
	a.Equal(1, run.Removed)
	item, ok := f.rm.Item(doc.RemarkableID)
	r.True(ok)
	a.Equal(remarkable.TrashID, item.Metadata.Parent)
	a.Equal([]string{"clinical"}, f.zot.Tags(paper.Key))
	doc, err = f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	a.Equal(db.StateRemoved, doc.State)

	f.zot.SetTags(paper.Key, "clinical", "send-to-remarkable")
	run = f.run(t)
	a.Equal(1, run.Pushed)
	doc, err = f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	a.Equal(db.StateSynced, doc.State)
	a.Len(f.folderChildren(t), 1)
}

func TestDeletedOnDeviceClearsTag(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	f := newFixture(t)
	paper := f.zot.AddPaper("Paper", "send-to-remarkable")
	att := f.zot.AddPDF(paper.Key, "paper.pdf", f.pdf)
	f.run(t)
	doc, err := f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	f.rm.Remove(doc.RemarkableID)
	run := f.run(t)
	a.Equal(1, run.Removed)
	a.Equal([]string{"zotrem:removed"}, f.zot.Tags(paper.Key))
	doc, err = f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	a.Equal(db.StateRemoved, doc.State)
	a.Equal(0, f.run(t).Removed)
}

func TestDocumentErrorsDoNotAbortRun(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	f := newFixture(t)
	broken := f.zot.AddPaper("Broken", "send-to-remarkable")
	brokenAtt := f.zot.AddItem(map[string]any{
		"contentType": "application/pdf",
		"filename":    "missing.pdf",
		"itemType":    "attachment",
		"linkMode":    "imported_file",
		"parentItem":  broken.Key,
	})
	good := f.zot.AddPaper("Good", "send-to-remarkable")
	f.zot.AddPDF(good.Key, "good.pdf", f.pdf)
	run := f.run(t)
	a.Equal(1, run.Pushed)
	doc, err := f.store.DocumentByZoteroKey(brokenAtt.Key)
	r.NoError(err)
	a.Equal(db.StateError, doc.State)
	a.Contains(doc.LastError, "download pdf")
	a.Equal([]string{"send-to-remarkable", "zotrem:error"}, f.zot.Tags(broken.Key))
	events, err := f.store.ListSyncEvents(run.ID)
	r.NoError(err)
	a.NotEmpty(events)
	a.False(f.engine.Running())
}

func TestOptionsCanDisableWrites(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	f := newFixture(t)
	settings, err := f.store.Settings()
	r.NoError(err)
	settings.AnnotatedPDF = false
	settings.WriteAnnotations = false
	r.NoError(f.store.SaveSettings(settings))
	paper := f.zot.AddPaper("Paper", "send-to-remarkable")
	att := f.zot.AddPDF(paper.Key, "paper.pdf", f.pdf)
	f.run(t)
	doc, err := f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	f.rm.SetPageStrokes(doc.RemarkableID, 0, loadRM(t, "rm2pdf_version6.rm"))
	run := f.run(t)
	a.Equal(1, run.Pulled)
	a.Empty(f.zot.ChildrenOf(att.Key))
	a.Len(f.zot.ChildrenOf(paper.Key), 1)
	doc, err = f.store.DocumentByZoteroKey(att.Key)
	r.NoError(err)
	a.Equal(26, doc.AnnotationCount)
	a.Equal(db.StateAnnotated, doc.State)
	a.True(zotero.ItemData{Tags: []zotero.Tag{{Tag: "zotrem:annotated"}}}.HasTag("ZOTREM:ANNOTATED"))
}
