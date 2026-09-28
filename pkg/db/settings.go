package db

import (
	"context"
	"strconv"

	"github.com/cockroachdb/errors"
	"github.com/kenners/zotrem/pkg/db/internal/sqlite"
	"github.com/kenners/zotrem/pkg/ts"
)

const (
	KeyAnnotatedPDF          = "annotated_pdf"
	KeyHealthcheckURL        = "healthcheck_url"
	KeyRemarkableDeviceToken = "remarkable_device_token"
	KeyRemarkableFolder      = "remarkable_folder"
	KeyRemarkableUserToken   = "remarkable_user_token"
	KeySyncIntervalMinutes   = "sync_interval_minutes"
	KeyTagPrefix             = "tag_prefix"
	KeyTagSend               = "tag_send"
	KeyTrashWhenUntagged     = "trash_when_untagged"
	KeyUntagWhenDeleted      = "untag_when_deleted"
	KeyWriteAnnotations      = "write_annotations"
	KeyZoteroAPIKey          = "zotero_api_key"
	KeyZoteroLibraryID       = "zotero_library_id"
	KeyZoteroLibraryType     = "zotero_library_type"
	KeyZoteroUsername        = "zotero_username"
)

type Settings struct {
	AnnotatedPDF          bool
	HealthcheckURL        string
	RemarkableDeviceToken string
	RemarkableFolder      string
	RemarkableUserToken   string
	SyncIntervalMinutes   int
	TagPrefix             string
	TagSend               string
	TrashWhenUntagged     bool
	UntagWhenDeleted      bool
	WriteAnnotations      bool
	ZoteroAPIKey          string
	ZoteroLibraryID       int64
	ZoteroLibraryType     string
	ZoteroUsername        string
}

func DefaultSettings() Settings {
	return Settings{
		AnnotatedPDF:        true,
		RemarkableFolder:    "Zotero",
		SyncIntervalMinutes: 15,
		TagPrefix:           "zotrem:",
		TagSend:             "send-to-remarkable",
		TrashWhenUntagged:   true,
		UntagWhenDeleted:    true,
		WriteAnnotations:    true,
		ZoteroLibraryType:   "user",
	}
}

func (s Settings) ZoteroConfigured() bool { return s.ZoteroAPIKey != "" && s.ZoteroLibraryID != 0 }

func (s Settings) RemarkableConfigured() bool { return s.RemarkableDeviceToken != "" }

func (s Settings) Configured() bool { return s.ZoteroConfigured() && s.RemarkableConfigured() }

func (s Settings) StateTag(state string) string { return s.TagPrefix + state }

func (s Settings) values() map[string]string {
	return map[string]string{
		KeyAnnotatedPDF:          strconv.FormatBool(s.AnnotatedPDF),
		KeyHealthcheckURL:        s.HealthcheckURL,
		KeyRemarkableDeviceToken: s.RemarkableDeviceToken,
		KeyRemarkableFolder:      s.RemarkableFolder,
		KeyRemarkableUserToken:   s.RemarkableUserToken,
		KeySyncIntervalMinutes:   strconv.Itoa(s.SyncIntervalMinutes),
		KeyTagPrefix:             s.TagPrefix,
		KeyTagSend:               s.TagSend,
		KeyTrashWhenUntagged:     strconv.FormatBool(s.TrashWhenUntagged),
		KeyUntagWhenDeleted:      strconv.FormatBool(s.UntagWhenDeleted),
		KeyWriteAnnotations:      strconv.FormatBool(s.WriteAnnotations),
		KeyZoteroAPIKey:          s.ZoteroAPIKey,
		KeyZoteroLibraryID:       strconv.FormatInt(s.ZoteroLibraryID, 10),
		KeyZoteroLibraryType:     s.ZoteroLibraryType,
		KeyZoteroUsername:        s.ZoteroUsername,
	}
}

func settingsFromRows(rows []sqlite.Setting) Settings {
	s := DefaultSettings()
	for _, row := range rows {
		switch row.Key {
		case KeyAnnotatedPDF:
			s.AnnotatedPDF = row.Value == "true"
		case KeyHealthcheckURL:
			s.HealthcheckURL = row.Value
		case KeyRemarkableDeviceToken:
			s.RemarkableDeviceToken = row.Value
		case KeyRemarkableFolder:
			s.RemarkableFolder = row.Value
		case KeyRemarkableUserToken:
			s.RemarkableUserToken = row.Value
		case KeySyncIntervalMinutes:
			s.SyncIntervalMinutes, _ = strconv.Atoi(row.Value)
		case KeyTagPrefix:
			s.TagPrefix = row.Value
		case KeyTagSend:
			s.TagSend = row.Value
		case KeyTrashWhenUntagged:
			s.TrashWhenUntagged = row.Value == "true"
		case KeyUntagWhenDeleted:
			s.UntagWhenDeleted = row.Value == "true"
		case KeyWriteAnnotations:
			s.WriteAnnotations = row.Value == "true"
		case KeyZoteroAPIKey:
			s.ZoteroAPIKey = row.Value
		case KeyZoteroLibraryID:
			s.ZoteroLibraryID, _ = strconv.ParseInt(row.Value, 10, 64)
		case KeyZoteroLibraryType:
			s.ZoteroLibraryType = row.Value
		case KeyZoteroUsername:
			s.ZoteroUsername = row.Value
		}
	}
	return s
}

func (d *DB) Settings() (Settings, error) {
	rows, err := d.queries.ListSettings(context.Background())
	if err != nil {
		return Settings{}, errors.Wrap(err, "list settings")
	}
	return settingsFromRows(rows), nil
}

func (d *DB) SaveSettings(s Settings) error {
	now := ts.Now()
	for key, value := range s.values() {
		if err := d.queries.UpsertSetting(context.Background(), sqlite.UpsertSettingParams{Key: key, UpdatedAt: now, Value: value}); err != nil {
			return errors.Wrapf(err, "save setting %s", key)
		}
	}
	return nil
}

func (d *DB) SetSetting(key, value string) error {
	if err := d.queries.UpsertSetting(context.Background(), sqlite.UpsertSettingParams{Key: key, UpdatedAt: ts.Now(), Value: value}); err != nil {
		return errors.Wrapf(err, "save setting %s", key)
	}
	return nil
}
