package remarkable_test

import (
	"context"
	"testing"
	"time"

	"github.com/kenners/zotrem/pkg/remarkable"
	"github.com/kenners/zotrem/pkg/remarkable/remarkabletest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newClient(t *testing.T, srv *remarkabletest.Server) *remarkable.Client {
	t.Helper()
	t.Cleanup(srv.Close)
	return remarkable.New(srv.DeviceToken,
		remarkable.WithAuthHost(srv.URL),
		remarkable.WithSyncHost(srv.URL),
		remarkable.WithSleep(func(time.Duration) {}),
	)
}

func TestRegister(t *testing.T) {
	a := assert.New(t)
	srv := remarkabletest.New()
	t.Cleanup(srv.Close)
	token, err := remarkable.Register(context.Background(), " ABCDEFGH "[1:9], remarkable.WithAuthHost(srv.URL))
	a.Error(err)
	a.Empty(token)
	token, err = remarkable.Register(context.Background(), srv.PairingCode, remarkable.WithAuthHost(srv.URL))
	a.NoError(err)
	a.Equal(srv.DeviceToken, token)
	_, err = remarkable.Register(context.Background(), "short", remarkable.WithAuthHost(srv.URL))
	a.ErrorContains(err, "8 characters")
}

func TestIndexRoundTrip(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	files := []remarkable.Entry{
		remarkable.FileEntry("doc.metadata", []byte(`{"a":1}`)),
		remarkable.FileEntry("doc.content", []byte(`{}`)),
	}
	for _, schema := range []int{3, 4} {
		data, entry, err := remarkable.EncodeIndex("doc", files, schema)
		r.NoError(err)
		idx, err := remarkable.ParseIndex(string(data))
		r.NoError(err)
		a.Equal(schema, idx.Schema)
		r.Len(idx.Entries, 2)
		a.Equal("doc.content", idx.Entries[0].ID)
		a.Equal("doc.metadata", idx.Entries[1].ID)
		a.Equal(2, entry.Subfiles)
		a.Equal(int64(9), entry.Size)
		a.Len(entry.Hash, 64)
		if schema == 3 {
			a.Equal("80000000", entry.Type)
		} else {
			a.Equal("0", entry.Type)
			a.Equal(remarkable.HashBytes(data), entry.Hash)
		}
	}
	_, err := remarkable.ParseIndex("5\n")
	a.Error(err)
}

func TestListAndFolders(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := remarkabletest.New()
	c := newClient(t, srv)
	folderID := srv.AddFolder("Zotero", "")
	docID := srv.AddPDF("paper.pdf", folderID, []byte("%PDF"), 2)
	root, items, err := c.ListItems(context.Background(), nil)
	r.NoError(err)
	a.Equal(4, root.SchemaVersion)
	r.Len(items, 2)
	folder, ok := remarkable.FindFolder(items, "zotero", "")
	a.True(ok)
	a.Equal(folderID, folder.ID)
	var doc remarkable.Item
	for _, item := range items {
		if item.ID == docID {
			doc = item
		}
	}
	a.Equal("paper.pdf", doc.VisibleName)
	a.Equal("pdf", doc.FileType)
	a.Equal(folderID, doc.Parent)
	a.False(doc.IsFolder())

	opened, err := c.OpenDocument(context.Background(), doc.Entry)
	r.NoError(err)
	content, err := opened.Content(context.Background())
	r.NoError(err)
	pages := content.PageOrder()
	r.Len(pages, 2)
	a.Equal(0, pages[0].PDFPage)
	a.Equal(1, pages[1].PDFPage)
	pdf, err := opened.PDF(context.Background())
	r.NoError(err)
	a.Equal("%PDF", string(pdf))
	_, ok, err = opened.PageFile(context.Background(), pages[0].ID)
	r.NoError(err)
	a.False(ok)

	srv.SetPageStrokes(docID, 1, []byte("strokes"))
	_, items, err = c.ListItems(context.Background(), nil)
	r.NoError(err)
	for _, item := range items {
		if item.ID == docID {
			a.NotEqual(doc.Hash, item.Hash, "hash must change when a page changes")
			opened, err = c.OpenDocument(context.Background(), item.Entry)
			r.NoError(err)
		}
	}
	data, ok, err := opened.PageFile(context.Background(), pages[1].ID)
	r.NoError(err)
	a.True(ok)
	a.Equal("strokes", string(data))
}

type mapCache struct{ items map[string]remarkable.Item }

func (m *mapCache) CachedItem(id, hash string) (remarkable.Item, bool) {
	item, ok := m.items[id]
	return item, ok && item.Hash == hash
}

func (m *mapCache) CacheItem(item remarkable.Item) { m.items[item.ID] = item }

func TestListUsesCache(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := remarkabletest.New()
	c := newClient(t, srv)
	srv.AddFolder("Zotero", "")
	cache := &mapCache{items: map[string]remarkable.Item{}}
	_, _, err := c.ListItems(context.Background(), cache)
	r.NoError(err)
	before := len(srv.Requests)
	_, items, err := c.ListItems(context.Background(), cache)
	r.NoError(err)
	a.Len(items, 1)
	a.Equal(before+2, len(srv.Requests), "cached listing should only fetch the root and its index")
}

func TestPutPDFAndMove(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := remarkabletest.New()
	c := newClient(t, srv)
	folder, err := c.CreateFolder(context.Background(), "Zotero", "")
	r.NoError(err)
	entry, err := c.PutPDF(context.Background(), "My paper", []byte("%PDF-1.4"), remarkable.PutPDFOptions{Parent: folder.ID})
	r.NoError(err)
	item, ok := srv.Item(entry.ID)
	r.True(ok)
	a.Equal("My paper", item.Metadata.VisibleName)
	a.Equal(folder.ID, item.Metadata.Parent)
	a.Equal(remarkable.TypeDocument, item.Metadata.Type)
	a.Equal("pdf", item.Content.FileType)
	a.Equal("%PDF-1.4", string(item.Files[entry.ID+".pdf"]))
	folderItem, ok := srv.Item(folder.ID)
	r.True(ok)
	a.Equal(remarkable.TypeCollection, folderItem.Metadata.Type)

	_, err = c.Trash(context.Background(), entry.ID)
	r.NoError(err)
	item, _ = srv.Item(entry.ID)
	a.Equal(remarkable.TrashID, item.Metadata.Parent)
	a.Equal("My paper", item.Metadata.VisibleName)
	_, err = c.Move(context.Background(), "missing", "")
	a.ErrorIs(err, remarkable.ErrNotFound)
}

func TestGenerationConflictRetries(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := remarkabletest.New()
	c := newClient(t, srv)
	conflicts := 0
	client := remarkable.New(srv.DeviceToken,
		remarkable.WithAuthHost(srv.URL),
		remarkable.WithSyncHost(srv.URL),
		remarkable.WithSleep(func(time.Duration) {
			if conflicts == 0 {
				srv.AddFolder("Concurrent", "")
			}
			conflicts++
		}),
	)
	_ = c
	folder, err := client.CreateFolder(context.Background(), "Zotero", "")
	r.NoError(err)
	items := srv.Items()
	a.Len(items, 1)
	a.Contains(items, folder.ID)
	srv2 := remarkabletest.New()
	t.Cleanup(srv2.Close)
	racing := remarkable.New(srv2.DeviceToken, remarkable.WithAuthHost(srv2.URL), remarkable.WithSyncHost(srv2.URL),
		remarkable.WithSleep(func(time.Duration) { srv2.AddFolder("Other", "") }))
	before := len(srv2.Items())
	_, err = racing.CreateFolder(context.Background(), "Zotero", "")
	r.NoError(err)
	a.Equal(before+1, len(srv2.Items())-countNamed(srv2, "Other"))
}

func countNamed(srv *remarkabletest.Server, name string) int {
	n := 0
	for _, item := range srv.Items() {
		if item.Metadata.VisibleName == name {
			n++
		}
	}
	return n
}

func TestUserTokenRefresh(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	srv := remarkabletest.New()
	var saved string
	client := remarkable.New(srv.DeviceToken,
		remarkable.WithAuthHost(srv.URL),
		remarkable.WithSyncHost(srv.URL),
		remarkable.WithUserToken("stale.token.value", func(token string) { saved = token }),
		remarkable.WithSleep(func(time.Duration) {}),
	)
	t.Cleanup(srv.Close)
	_, err := client.Root(context.Background())
	r.NoError(err)
	a.Equal(srv.UserToken, saved)
	a.Equal(1, srv.TokenRefreshes())
	srv.ExpireUserToken()
	_, err = client.Root(context.Background())
	r.NoError(err)
	a.Equal(2, srv.TokenRefreshes())

	bad := remarkable.New("wrong", remarkable.WithAuthHost(srv.URL), remarkable.WithSyncHost(srv.URL), remarkable.WithSleep(func(time.Duration) {}))
	_, err = bad.Root(context.Background())
	a.ErrorIs(err, remarkable.ErrUnauthorized)
}

func TestPageOrderLegacy(t *testing.T) {
	a := assert.New(t)
	content := remarkable.Content{FileType: "pdf", Pages: []string{"a", "b", "c"}, RedirectionPageMap: []int{0, -1, 1}}
	pages := content.PageOrder()
	a.Equal([]remarkable.Page{{ID: "a", Index: 0, PDFPage: 0}, {ID: "b", Index: 1, PDFPage: -1}, {ID: "c", Index: 2, PDFPage: 1}}, pages)
	deleted := remarkable.Content{CPages: &remarkable.CPages{Pages: []remarkable.CPage{
		{ID: "z", Idx: remarkable.Timestamped[string]{Value: "bb"}, Redir: &remarkable.Timestamped[int]{Value: 1}},
		{ID: "y", Idx: remarkable.Timestamped[string]{Value: "ba"}, Deleted: &remarkable.Timestamped[int]{Value: 1}},
		{ID: "x", Idx: remarkable.Timestamped[string]{Value: "aa"}, Redir: &remarkable.Timestamped[int]{Value: 0}},
	}}}
	pages = deleted.PageOrder()
	a.Equal([]remarkable.Page{{ID: "x", Index: 0, PDFPage: 0}, {ID: "z", Index: 1, PDFPage: 1}}, pages)
}
