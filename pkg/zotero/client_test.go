package zotero_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/kenners/zotrem/pkg/zotero"
	"github.com/kenners/zotrem/pkg/zotero/zoterotest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newClient(t *testing.T, srv *zoterotest.Server) *zotero.Client {
	t.Helper()
	t.Cleanup(srv.Close)
	return zotero.New(srv.APIKey,
		zotero.WithBaseURL(srv.URL),
		zotero.WithLibrary("user", srv.UserID),
		zotero.WithSleep(func(time.Duration) {}),
	)
}

func TestCurrentKey(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := zoterotest.New()
	c := newClient(t, srv)
	info, err := c.CurrentKey(context.Background())
	r.NoError(err)
	a.Equal(srv.UserID, info.UserID)
	a.True(info.Access.User.Write)
}

func TestBadKey(t *testing.T) {
	srv := zoterotest.New()
	t.Cleanup(srv.Close)
	c := zotero.New("wrong", zotero.WithBaseURL(srv.URL), zotero.WithSleep(func(time.Duration) {}))
	_, err := c.CurrentKey(context.Background())
	assert.Equal(t, http.StatusForbidden, zotero.StatusOf(err))
}

func TestItemsByTagPaginates(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := zoterotest.New()
	srv.PageSize = 2
	c := newClient(t, srv)
	for i := range 5 {
		srv.AddPaper("paper", "send-to-remarkable")
		if i%2 == 0 {
			srv.AddPaper("other", "other")
		}
	}
	items, err := c.ItemsByTag(context.Background(), "Send-To-Remarkable")
	r.NoError(err)
	a.Len(items, 5)
	for _, item := range items {
		a.True(item.Data.HasTag("send-to-remarkable"))
		a.True(item.Data.IsRegularItem())
	}
}

func TestChildrenAndDownload(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := zoterotest.New()
	c := newClient(t, srv)
	paper := srv.AddPaper("paper", "send-to-remarkable")
	pdf := srv.AddPDF(paper.Key, "paper.pdf", []byte("%PDF-1.4 fake"))
	children, err := c.Children(context.Background(), paper.Key)
	r.NoError(err)
	r.Len(children, 1)
	a.Equal(pdf.Key, children[0].Key)
	a.True(children[0].Data.IsStoredPDF())
	data, info, err := c.DownloadFile(context.Background(), pdf.Key)
	r.NoError(err)
	a.Equal("%PDF-1.4 fake", string(data))
	a.Equal(zotero.MD5Hex(data), info.MD5)
	a.Equal(int64(13), info.Size)
}

func TestUploadNewAndReplace(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := zoterotest.New()
	c := newClient(t, srv)
	paper := srv.AddPaper("paper")
	created, err := c.CreateItems(context.Background(), []zotero.ItemData{{
		ContentType: "application/pdf",
		Filename:    "annotated.pdf",
		ItemType:    zotero.ItemTypeAttachment,
		LinkMode:    zotero.LinkModeImportedFile,
		ParentItem:  paper.Key,
		Title:       "annotated.pdf",
	}})
	r.NoError(err)
	r.Len(created, 1)
	att := created[0]
	first := []byte("first version")
	version, err := c.UploadFile(context.Background(), zotero.UploadRequest{ContentType: "application/pdf", Data: first, Filename: "annotated.pdf", Key: att.Key})
	r.NoError(err)
	a.Positive(version)
	a.Equal(first, srv.Files[att.Key])
	a.Equal(zotero.MD5Hex(first), srv.Item(att.Key).Data["md5"])

	_, err = c.UploadFile(context.Background(), zotero.UploadRequest{Data: []byte("x"), Filename: "annotated.pdf", Key: att.Key})
	a.True(zotero.IsPreconditionFailed(err), "uploading a new file over an existing one must fail: %v", err)

	second := []byte("second version")
	version2, err := c.UploadFile(context.Background(), zotero.UploadRequest{Data: second, ExistingMD5: zotero.MD5Hex(first), Filename: "annotated.pdf", Key: att.Key})
	r.NoError(err)
	a.Greater(version2, version)
	a.Equal(second, srv.Files[att.Key])

	other := srv.AddPDF(paper.Key, "copy.pdf", second)
	_ = other
	third, err := c.CreateItems(context.Background(), []zotero.ItemData{{ContentType: "application/pdf", Filename: "dup.pdf", ItemType: zotero.ItemTypeAttachment, LinkMode: zotero.LinkModeImportedFile, ParentItem: paper.Key}})
	r.NoError(err)
	_, err = c.UploadFile(context.Background(), zotero.UploadRequest{Data: second, Filename: "dup.pdf", Key: third[0].Key})
	r.NoError(err)
	a.Equal(second, srv.Files[third[0].Key])
}

func TestPatchTagsAndVersionConflict(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := zoterotest.New()
	c := newClient(t, srv)
	paper := srv.AddPaper("paper", "send-to-remarkable")
	item, err := c.Item(context.Background(), paper.Key)
	r.NoError(err)
	tags := zotero.WithTag(item.Data.Tags, "zotrem:synced")
	version, err := c.SetTags(context.Background(), item, tags)
	r.NoError(err)
	a.Greater(version, item.Version)
	a.Equal([]string{"send-to-remarkable", "zotrem:synced"}, srv.Tags(paper.Key))

	_, err = c.SetTags(context.Background(), item, zotero.WithoutTag(tags, "send-to-remarkable"))
	a.True(zotero.IsPreconditionFailed(err))
}

func TestCreateAnnotationsAndDelete(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := zoterotest.New()
	c := newClient(t, srv)
	paper := srv.AddPaper("paper")
	pdf := srv.AddPDF(paper.Key, "paper.pdf", []byte("pdf"))
	position, _ := json.Marshal(map[string]any{"pageIndex": 0, "paths": [][]float64{{1, 2, 3, 4}}, "width": 2})
	created, err := c.CreateItems(context.Background(), []zotero.ItemData{{
		AnnotationColor:     "#ff0000",
		AnnotationPosition:  string(position),
		AnnotationSortIndex: "00000|000000|00010",
		AnnotationType:      zotero.AnnotationInk,
		ItemType:            zotero.ItemTypeAnnotation,
		ParentItem:          pdf.Key,
	}})
	r.NoError(err)
	r.Len(created, 1)
	a.Equal(zotero.AnnotationInk, created[0].Data.AnnotationType)
	children, err := c.Children(context.Background(), pdf.Key)
	r.NoError(err)
	a.Len(children, 1)
	r.NoError(c.DeleteItem(context.Background(), created[0].Key, created[0].Version))
	children, err = c.Children(context.Background(), pdf.Key)
	r.NoError(err)
	a.Empty(children)
	r.NoError(c.DeleteItem(context.Background(), created[0].Key, created[0].Version))
}

func TestCreateItemsReportsFailures(t *testing.T) {
	srv := zoterotest.New()
	c := newClient(t, srv)
	_, err := c.CreateItems(context.Background(), []zotero.ItemData{{ItemType: zotero.ItemTypeAnnotation, ParentItem: "MISSING"}})
	assert.ErrorContains(t, err, "rejected item 0")
}

func TestRetriesTransientErrors(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := zoterotest.New()
	c := newClient(t, srv)
	srv.FailNext(http.StatusTooManyRequests, http.StatusServiceUnavailable)
	info, err := c.CurrentKey(context.Background())
	r.NoError(err)
	a.Equal(srv.UserID, info.UserID)
	a.Len(srv.Requests, 3)
}
