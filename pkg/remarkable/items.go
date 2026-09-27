package remarkable

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
)

const (
	TypeCollection = "CollectionType"
	TypeDocument   = "DocumentType"
)

type Metadata struct {
	CreatedTime    string `json:"createdTime,omitempty"`
	Deleted        bool   `json:"deleted,omitempty"`
	LastModified   string `json:"lastModified"`
	LastOpened     string `json:"lastOpened,omitempty"`
	LastOpenedPage int    `json:"lastOpenedPage"`
	Parent         string `json:"parent"`
	Pinned         bool   `json:"pinned"`
	Type           string `json:"type"`
	VisibleName    string `json:"visibleName"`
}

type Timestamped[T any] struct {
	Timestamp string `json:"timestamp"`
	Value     T      `json:"value"`
}

type CPage struct {
	Deleted  *Timestamped[int]    `json:"deleted,omitempty"`
	ID       string               `json:"id"`
	Idx      Timestamped[string]  `json:"idx"`
	Redir    *Timestamped[int]    `json:"redir,omitempty"`
	Template *Timestamped[string] `json:"template,omitempty"`
}

type CPages struct {
	Pages []CPage `json:"pages"`
}

type Content struct {
	CPages             *CPages  `json:"cPages,omitempty"`
	FileType           string   `json:"fileType"`
	OriginalPageCount  int      `json:"originalPageCount,omitempty"`
	Orientation        string   `json:"orientation,omitempty"`
	PageCount          int      `json:"pageCount"`
	Pages              []string `json:"pages,omitempty"`
	RedirectionPageMap []int    `json:"redirectionPageMap,omitempty"`
	ZoomMode           string   `json:"zoomMode,omitempty"`
}

type Page struct {
	ID      string
	Index   int
	PDFPage int
}

func (c Content) PageOrder() []Page {
	if c.CPages != nil && len(c.CPages.Pages) > 0 {
		pages := make([]CPage, 0, len(c.CPages.Pages))
		for _, p := range c.CPages.Pages {
			if p.Deleted == nil || p.Deleted.Value == 0 {
				pages = append(pages, p)
			}
		}
		sort.SliceStable(pages, func(i, j int) bool { return pages[i].Idx.Value < pages[j].Idx.Value })
		out := make([]Page, 0, len(pages))
		for i, p := range pages {
			page := Page{ID: p.ID, Index: i, PDFPage: -1}
			if p.Redir != nil {
				page.PDFPage = p.Redir.Value
			}
			out = append(out, page)
		}
		return out
	}
	out := make([]Page, 0, len(c.Pages))
	for i, id := range c.Pages {
		page := Page{ID: id, Index: i, PDFPage: -1}
		if i < len(c.RedirectionPageMap) {
			page.PDFPage = c.RedirectionPageMap[i]
		} else if c.FileType == "pdf" || c.FileType == "epub" {
			page.PDFPage = i
		}
		out = append(out, page)
	}
	return out
}

type Item struct {
	Entry
	FileType     string
	LastModified string
	Parent       string
	Pinned       bool
	VisibleName  string
	ItemType     string
}

func (i Item) IsFolder() bool { return i.ItemType == TypeCollection }

func (i Item) IsTrashed() bool { return i.Parent == TrashID }

func (c *Client) RootIndex(ctx context.Context) (Root, Index, error) {
	root, err := c.Root(ctx)
	if err != nil {
		return Root{}, Index{}, err
	}
	data, err := c.GetBlob(ctx, root.Hash, "root.docSchema")
	if err != nil {
		return Root{}, Index{}, errors.Wrap(err, "fetch root index")
	}
	idx, err := ParseIndex(string(data))
	if err != nil {
		return Root{}, Index{}, errors.Wrap(err, "parse root index")
	}
	return root, idx, nil
}

func (c *Client) ItemIndex(ctx context.Context, entry Entry) (Index, error) {
	data, err := c.GetBlob(ctx, entry.Hash, entry.ID+".docSchema")
	if err != nil {
		return Index{}, errors.Wrapf(err, "fetch index of %s", entry.ID)
	}
	idx, err := ParseIndex(string(data))
	if err != nil {
		return Index{}, errors.Wrapf(err, "parse index of %s", entry.ID)
	}
	return idx, nil
}

type Document struct {
	Client *Client
	Entry  Entry
	Files  map[string]Entry
	Index  Index
}

func (c *Client) OpenDocument(ctx context.Context, entry Entry) (*Document, error) {
	idx, err := c.ItemIndex(ctx, entry)
	if err != nil {
		return nil, err
	}
	doc := &Document{Client: c, Entry: entry, Files: map[string]Entry{}, Index: idx}
	for _, e := range idx.Entries {
		doc.Files[e.ID] = e
	}
	return doc, nil
}

func (d *Document) Has(name string) bool {
	_, ok := d.Files[name]
	return ok
}

func (d *Document) Blob(ctx context.Context, name string) ([]byte, error) {
	e, ok := d.Files[name]
	if !ok {
		return nil, errors.Wrapf(ErrNotFound, "%s has no file %s", d.Entry.ID, name)
	}
	return d.Client.GetBlob(ctx, e.Hash, name)
}

func (d *Document) Metadata(ctx context.Context) (Metadata, error) {
	data, err := d.Blob(ctx, d.Entry.ID+".metadata")
	if err != nil {
		return Metadata{}, err
	}
	var meta Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return Metadata{}, errors.Wrapf(err, "decode metadata of %s", d.Entry.ID)
	}
	return meta, nil
}

func (d *Document) Content(ctx context.Context) (Content, error) {
	data, err := d.Blob(ctx, d.Entry.ID+".content")
	if err != nil {
		return Content{}, err
	}
	var content Content
	if err := json.Unmarshal(data, &content); err != nil {
		return Content{}, errors.Wrapf(err, "decode content of %s", d.Entry.ID)
	}
	return content, nil
}

func (d *Document) PDF(ctx context.Context) ([]byte, error) {
	return d.Blob(ctx, d.Entry.ID+".pdf")
}

func (d *Document) PageFile(ctx context.Context, pageID string) ([]byte, bool, error) {
	name := d.Entry.ID + "/" + pageID + ".rm"
	if !d.Has(name) {
		return nil, false, nil
	}
	data, err := d.Blob(ctx, name)
	return data, true, err
}

func (c *Client) ItemFromEntry(ctx context.Context, entry Entry) (Item, error) {
	doc, err := c.OpenDocument(ctx, entry)
	if err != nil {
		return Item{}, err
	}
	meta, err := doc.Metadata(ctx)
	if err != nil {
		return Item{}, err
	}
	item := Item{
		Entry:        entry,
		ItemType:     meta.Type,
		LastModified: meta.LastModified,
		Parent:       meta.Parent,
		Pinned:       meta.Pinned,
		VisibleName:  meta.VisibleName,
	}
	if meta.Type == TypeDocument {
		if data, err := doc.Blob(ctx, entry.ID+".content"); err == nil {
			var content Content
			if json.Unmarshal(data, &content) == nil {
				item.FileType = content.FileType
			}
		}
	}
	return item, nil
}

type ItemCache interface {
	CachedItem(id, hash string) (Item, bool)
	CacheItem(item Item)
}

func (c *Client) ListItems(ctx context.Context, cache ItemCache) (Root, []Item, error) {
	root, idx, err := c.RootIndex(ctx)
	if err != nil {
		return Root{}, nil, err
	}
	items := make([]Item, 0, len(idx.Entries))
	for _, entry := range idx.Entries {
		if cache != nil {
			if item, ok := cache.CachedItem(entry.ID, entry.Hash); ok {
				items = append(items, item)
				continue
			}
		}
		item, err := c.ItemFromEntry(ctx, entry)
		if err != nil {
			c.log.Warn("skipping unreadable remarkable item", "id", entry.ID, "error", err)
			continue
		}
		if cache != nil {
			cache.CacheItem(item)
		}
		items = append(items, item)
	}
	return root, items, nil
}

func FindFolder(items []Item, name, parent string) (Item, bool) {
	for _, item := range items {
		if item.IsFolder() && item.Parent == parent && strings.EqualFold(item.VisibleName, name) {
			return item, true
		}
	}
	return Item{}, false
}
