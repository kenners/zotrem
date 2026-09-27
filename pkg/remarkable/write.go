package remarkable

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
)

const maxGenerationRetries = 5

type documentContent struct {
	CoverPageNumber    int               `json:"coverPageNumber"`
	DocumentMetadata   map[string]any    `json:"documentMetadata"`
	ExtraMetadata      map[string]string `json:"extraMetadata"`
	FileType           string            `json:"fileType"`
	FontName           string            `json:"fontName"`
	FormatVersion      int               `json:"formatVersion"`
	LineHeight         int               `json:"lineHeight"`
	Margins            int               `json:"margins"`
	Orientation        string            `json:"orientation"`
	OriginalPageCount  int               `json:"originalPageCount"`
	PageCount          int               `json:"pageCount"`
	PageTags           []any             `json:"pageTags"`
	Pages              []string          `json:"pages"`
	RedirectionPageMap []int             `json:"redirectionPageMap"`
	SizeInBytes        string            `json:"sizeInBytes"`
	Tags               []any             `json:"tags"`
	TextAlignment      string            `json:"textAlignment"`
	TextScale          int               `json:"textScale"`
	ZoomMode           string            `json:"zoomMode"`
}

func nowMillis() string { return strconv.FormatInt(time.Now().UnixMilli(), 10) }

func (c *Client) putJSON(ctx context.Context, name string, v any) (Entry, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Entry{}, errors.Wrapf(err, "encode %s", name)
	}
	if _, err := c.PutBlob(ctx, name, data); err != nil {
		return Entry{}, errors.Wrapf(err, "upload %s", name)
	}
	return FileEntry(name, data), nil
}

func (c *Client) putItemIndex(ctx context.Context, id string, files []Entry, schema int) (Entry, error) {
	data, entry, err := EncodeIndex(id, files, schema)
	if err != nil {
		return Entry{}, err
	}
	if _, err := c.PutBlob(ctx, id+".docSchema", data); err != nil {
		return Entry{}, errors.Wrapf(err, "upload index of %s", id)
	}
	return entry, nil
}

func (c *Client) updateRoot(ctx context.Context, mutate func(entries []Entry) ([]Entry, error)) (Root, error) {
	var lastErr error
	for attempt := 0; attempt < maxGenerationRetries; attempt++ {
		root, idx, err := c.RootIndex(ctx)
		if err != nil {
			return Root{}, err
		}
		entries, err := mutate(idx.Entries)
		if err != nil {
			return Root{}, err
		}
		data, rootEntry, err := EncodeIndex("root", entries, 4)
		if err != nil {
			return Root{}, err
		}
		if _, err := c.PutBlob(ctx, "root.docSchema", data); err != nil {
			return Root{}, errors.Wrap(err, "upload root index")
		}
		updated, err := c.PutRoot(ctx, rootEntry.Hash, root.Generation)
		if errors.Is(err, ErrGenerationConflict) {
			lastErr = err
			c.sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
			continue
		}
		if err != nil {
			return Root{}, err
		}
		return updated, nil
	}
	return Root{}, lastErr
}

func replaceEntry(entries []Entry, entry Entry) []Entry {
	out := make([]Entry, 0, len(entries)+1)
	for _, e := range entries {
		if e.ID != entry.ID {
			out = append(out, e)
		}
	}
	return append(out, entry)
}

type PutPDFOptions struct {
	Parent string
}

func (c *Client) PutPDF(ctx context.Context, visibleName string, pdf []byte, opts PutPDFOptions) (Entry, error) {
	root, err := c.Root(ctx)
	if err != nil {
		return Entry{}, err
	}
	id := uuid.NewString()
	now := nowMillis()
	metadata := Metadata{
		CreatedTime:    now,
		LastModified:   now,
		LastOpened:     "0",
		LastOpenedPage: 0,
		Parent:         opts.Parent,
		Pinned:         false,
		Type:           TypeDocument,
		VisibleName:    visibleName,
	}
	content := documentContent{
		CoverPageNumber:    -1,
		DocumentMetadata:   map[string]any{},
		ExtraMetadata:      map[string]string{},
		FileType:           "pdf",
		FontName:           "",
		FormatVersion:      1,
		LineHeight:         -1,
		Margins:            125,
		Orientation:        "portrait",
		OriginalPageCount:  1,
		PageCount:          1,
		PageTags:           []any{},
		Pages:              []string{uuid.NewString()},
		RedirectionPageMap: []int{0},
		SizeInBytes:        strconv.Itoa(len(pdf)),
		Tags:               []any{},
		TextAlignment:      "justify",
		TextScale:          1,
		ZoomMode:           "bestFit",
	}
	contentEntry, err := c.putJSON(ctx, id+".content", content)
	if err != nil {
		return Entry{}, err
	}
	metadataEntry, err := c.putJSON(ctx, id+".metadata", metadata)
	if err != nil {
		return Entry{}, err
	}
	pagedata := []byte("\n")
	if _, err := c.PutBlob(ctx, id+".pagedata", pagedata); err != nil {
		return Entry{}, errors.Wrap(err, "upload pagedata")
	}
	if _, err := c.PutBlob(ctx, id+".pdf", pdf); err != nil {
		return Entry{}, errors.Wrap(err, "upload pdf")
	}
	files := []Entry{contentEntry, metadataEntry, FileEntry(id+".pagedata", pagedata), FileEntry(id+".pdf", pdf)}
	docEntry, err := c.putItemIndex(ctx, id, files, root.SchemaVersion)
	if err != nil {
		return Entry{}, err
	}
	if _, err := c.updateRoot(ctx, func(entries []Entry) ([]Entry, error) {
		return replaceEntry(entries, docEntry), nil
	}); err != nil {
		return Entry{}, err
	}
	return docEntry, nil
}

func (c *Client) CreateFolder(ctx context.Context, visibleName, parent string) (Entry, error) {
	root, err := c.Root(ctx)
	if err != nil {
		return Entry{}, err
	}
	id := uuid.NewString()
	now := nowMillis()
	metadata := Metadata{
		CreatedTime:  now,
		LastModified: now,
		Parent:       parent,
		Pinned:       false,
		Type:         TypeCollection,
		VisibleName:  visibleName,
	}
	contentEntry, err := c.putJSON(ctx, id+".content", map[string]any{"tags": []any{}})
	if err != nil {
		return Entry{}, err
	}
	metadataEntry, err := c.putJSON(ctx, id+".metadata", metadata)
	if err != nil {
		return Entry{}, err
	}
	folderEntry, err := c.putItemIndex(ctx, id, []Entry{contentEntry, metadataEntry}, root.SchemaVersion)
	if err != nil {
		return Entry{}, err
	}
	if _, err := c.updateRoot(ctx, func(entries []Entry) ([]Entry, error) {
		return replaceEntry(entries, folderEntry), nil
	}); err != nil {
		return Entry{}, err
	}
	return folderEntry, nil
}

func (c *Client) EditMetadata(ctx context.Context, id string, edit func(*Metadata)) (Entry, error) {
	var updated Entry
	_, err := c.updateRoot(ctx, func(entries []Entry) ([]Entry, error) {
		var current *Entry
		for i := range entries {
			if entries[i].ID == id {
				current = &entries[i]
			}
		}
		if current == nil {
			return nil, errors.Wrapf(ErrNotFound, "item %s", id)
		}
		doc, err := c.OpenDocument(ctx, *current)
		if err != nil {
			return nil, err
		}
		meta, err := doc.Metadata(ctx)
		if err != nil {
			return nil, err
		}
		edit(&meta)
		meta.LastModified = nowMillis()
		metadataEntry, err := c.putJSON(ctx, id+".metadata", meta)
		if err != nil {
			return nil, err
		}
		files := make([]Entry, 0, len(doc.Index.Entries))
		for _, e := range doc.Index.Entries {
			if e.ID != metadataEntry.ID {
				files = append(files, e)
			}
		}
		files = append(files, metadataEntry)
		updated, err = c.putItemIndex(ctx, id, files, doc.Index.Schema)
		if err != nil {
			return nil, err
		}
		return replaceEntry(entries, updated), nil
	})
	return updated, err
}

func (c *Client) Move(ctx context.Context, id, parent string) (Entry, error) {
	return c.EditMetadata(ctx, id, func(m *Metadata) { m.Parent = parent })
}

func (c *Client) Trash(ctx context.Context, id string) (Entry, error) {
	return c.Move(ctx, id, TrashID)
}
