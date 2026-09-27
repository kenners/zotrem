package db

import (
	"context"
	"database/sql"

	"github.com/cockroachdb/errors"
	"github.com/kenners/zotrem/pkg/db/internal/sqlite"
	"github.com/kenners/zotrem/pkg/remarkable"
	"github.com/kenners/zotrem/pkg/ts"
)

func (d *DB) CachedItem(id, hash string) (remarkable.Item, bool) {
	row, err := d.queries.GetRemarkableItem(context.Background(), id)
	if err != nil || row.Hash != hash {
		return remarkable.Item{}, false
	}
	return remarkable.Item{
		Entry:        remarkable.Entry{Hash: row.Hash, ID: row.ID},
		FileType:     row.FileType,
		ItemType:     row.ItemType,
		LastModified: row.LastModified,
		Parent:       row.Parent,
		Pinned:       row.Pinned != 0,
		VisibleName:  row.VisibleName,
	}, true
}

func (d *DB) CacheItem(item remarkable.Item) {
	pinned := int64(0)
	if item.Pinned {
		pinned = 1
	}
	_ = d.queries.UpsertRemarkableItem(context.Background(), sqlite.UpsertRemarkableItemParams{
		FileType:     item.FileType,
		Hash:         item.Hash,
		ID:           item.ID,
		ItemType:     item.ItemType,
		LastModified: item.LastModified,
		Parent:       item.Parent,
		Pinned:       pinned,
		UpdatedAt:    ts.Now(),
		VisibleName:  item.VisibleName,
	})
}

func (d *DB) ClearRemarkableCache() error {
	err := d.queries.DeleteRemarkableItems(context.Background())
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return errors.Wrap(err, "clear remarkable cache")
}
