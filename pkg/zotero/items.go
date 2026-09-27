package zotero

import (
	"encoding/json"
	"slices"
	"strings"
)

const (
	ItemTypeAnnotation = "annotation"
	ItemTypeAttachment = "attachment"
	ItemTypeNote       = "note"
)

const (
	LinkModeImportedFile = "imported_file"
	LinkModeImportedURL  = "imported_url"
)

const (
	AnnotationHighlight = "highlight"
	AnnotationInk       = "ink"
	AnnotationNote      = "note"
	AnnotationUnderline = "underline"
)

type Tag struct {
	Tag  string `json:"tag"`
	Type int    `json:"type,omitempty"`
}

type Item struct {
	Data    ItemData `json:"data"`
	Key     string   `json:"key"`
	Meta    ItemMeta `json:"meta"`
	Version int      `json:"version"`
}

type ItemMeta struct {
	NumChildren int `json:"numChildren"`
}

type ItemData struct {
	AnnotationColor     string          `json:"annotationColor,omitempty"`
	AnnotationComment   string          `json:"annotationComment,omitempty"`
	AnnotationPageLabel string          `json:"annotationPageLabel,omitempty"`
	AnnotationPosition  string          `json:"annotationPosition,omitempty"`
	AnnotationSortIndex string          `json:"annotationSortIndex,omitempty"`
	AnnotationText      string          `json:"annotationText,omitempty"`
	AnnotationType      string          `json:"annotationType,omitempty"`
	ContentType         string          `json:"contentType,omitempty"`
	DateModified        string          `json:"dateModified,omitempty"`
	Deleted             int             `json:"deleted,omitempty"`
	Filename            string          `json:"filename,omitempty"`
	ItemType            string          `json:"itemType"`
	Key                 string          `json:"key,omitempty"`
	LinkMode            string          `json:"linkMode,omitempty"`
	MD5                 string          `json:"md5,omitempty"`
	Mtime               json.RawMessage `json:"mtime,omitempty"`
	Note                string          `json:"note,omitempty"`
	ParentItem          string          `json:"parentItem,omitempty"`
	Tags                []Tag           `json:"tags"`
	Title               string          `json:"title,omitempty"`
	Version             int             `json:"version,omitempty"`
}

func (d ItemData) HasTag(tag string) bool {
	for _, t := range d.Tags {
		if strings.EqualFold(t.Tag, tag) {
			return true
		}
	}
	return false
}

func (d ItemData) IsStoredPDF() bool {
	return d.ItemType == ItemTypeAttachment &&
		strings.EqualFold(d.ContentType, "application/pdf") &&
		(d.LinkMode == LinkModeImportedFile || d.LinkMode == LinkModeImportedURL)
}

func (d ItemData) IsRegularItem() bool {
	switch d.ItemType {
	case ItemTypeAnnotation, ItemTypeAttachment, ItemTypeNote:
		return false
	}
	return true
}

func WithTag(tags []Tag, tag string) []Tag {
	if slices.ContainsFunc(tags, func(t Tag) bool { return strings.EqualFold(t.Tag, tag) }) {
		return tags
	}
	out := slices.Clone(tags)
	return append(out, Tag{Tag: tag})
}

func WithoutTag(tags []Tag, tag string) []Tag {
	out := make([]Tag, 0, len(tags))
	for _, t := range tags {
		if !strings.EqualFold(t.Tag, tag) {
			out = append(out, t)
		}
	}
	return out
}

func TagsEqual(a, b []Tag) bool {
	if len(a) != len(b) {
		return false
	}
	as := make([]string, 0, len(a))
	bs := make([]string, 0, len(b))
	for _, t := range a {
		as = append(as, strings.ToLower(t.Tag))
	}
	for _, t := range b {
		bs = append(bs, strings.ToLower(t.Tag))
	}
	slices.Sort(as)
	slices.Sort(bs)
	return slices.Equal(as, bs)
}

func (i Item) Title() string {
	if i.Data.Title != "" {
		return i.Data.Title
	}
	if i.Data.Filename != "" {
		return i.Data.Filename
	}
	return i.Key
}
