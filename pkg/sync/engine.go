package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/kenners/zotrem/pkg/db"
	"github.com/kenners/zotrem/pkg/remarkable"
	"github.com/kenners/zotrem/pkg/render"
	"github.com/kenners/zotrem/pkg/rm"
	"github.com/kenners/zotrem/pkg/ts"
	"github.com/kenners/zotrem/pkg/zotero"
)

var ErrAlreadyRunning = errors.New("a sync is already running")

type Options struct {
	RemarkableAuthHost string
	RemarkableSyncHost string
	Sleep              func(time.Duration)
	ZoteroBaseURL      string
}

type Engine struct {
	log     *slog.Logger
	opts    Options
	running bool
	runMu   sync.Mutex
	stateMu sync.Mutex
	store   *db.DB
}

func New(store *db.DB, log *slog.Logger, opts Options) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{log: log, opts: opts, store: store}
}

func (e *Engine) Running() bool {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	return e.running
}

func (e *Engine) setRunning(v bool) {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	e.running = v
}

func (e *Engine) ZoteroClient(s db.Settings) *zotero.Client {
	opts := []zotero.Option{zotero.WithLogger(e.log)}
	if e.opts.ZoteroBaseURL != "" {
		opts = append(opts, zotero.WithBaseURL(e.opts.ZoteroBaseURL))
	}
	if e.opts.Sleep != nil {
		opts = append(opts, zotero.WithSleep(e.opts.Sleep))
	}
	if s.ZoteroLibraryID != 0 {
		opts = append(opts, zotero.WithLibrary(s.ZoteroLibraryType, s.ZoteroLibraryID))
	}
	return zotero.New(s.ZoteroAPIKey, opts...)
}

func (e *Engine) Register(ctx context.Context, code string) (string, error) {
	var opts []remarkable.Option
	if e.opts.RemarkableAuthHost != "" {
		opts = append(opts, remarkable.WithAuthHost(e.opts.RemarkableAuthHost))
	}
	return remarkable.Register(ctx, code, opts...)
}

func (e *Engine) RemarkableClient(s db.Settings) *remarkable.Client {
	opts := []remarkable.Option{
		remarkable.WithLogger(e.log),
		remarkable.WithUserToken(s.RemarkableUserToken, func(token string) {
			if err := e.store.SetSetting(db.KeyRemarkableUserToken, token); err != nil {
				e.log.Warn("persist remarkable user token", "error", err)
			}
		}),
	}
	if e.opts.RemarkableAuthHost != "" {
		opts = append(opts, remarkable.WithAuthHost(e.opts.RemarkableAuthHost))
	}
	if e.opts.RemarkableSyncHost != "" {
		opts = append(opts, remarkable.WithSyncHost(e.opts.RemarkableSyncHost))
	}
	if e.opts.Sleep != nil {
		opts = append(opts, remarkable.WithSleep(e.opts.Sleep))
	}
	return remarkable.New(s.RemarkableDeviceToken, opts...)
}

type run struct {
	ctx      context.Context
	engine   *Engine
	folder   remarkable.Item
	items    map[string]remarkable.Item
	rc       *remarkable.Client
	record   db.SyncRun
	settings db.Settings
	zc       *zotero.Client
}

func (e *Engine) Run(ctx context.Context, trigger string) (db.SyncRun, error) {
	if !e.runMu.TryLock() {
		return db.SyncRun{}, ErrAlreadyRunning
	}
	defer e.runMu.Unlock()
	e.setRunning(true)
	defer e.setRunning(false)

	record, err := e.store.StartSyncRun(trigger)
	if err != nil {
		return db.SyncRun{}, err
	}
	r := &run{ctx: ctx, engine: e, record: record}
	runErr := r.execute()
	if runErr != nil {
		r.record.Status = db.RunStatusError
		r.record.Error = runErr.Error()
		r.event(0, db.LevelError, "sync failed: "+runErr.Error())
	} else {
		r.record.Status = db.RunStatusOK
	}
	if err := e.store.FinishSyncRun(r.record); err != nil {
		return r.record, err
	}
	return r.record, runErr
}

func (r *run) event(documentID int64, level, message string) {
	if err := r.engine.store.AddSyncEvent(r.record.ID, documentID, level, message); err != nil {
		r.engine.log.Warn("record sync event", "error", err)
	}
	switch level {
	case db.LevelError:
		r.engine.log.Error(message, "run", r.record.ID, "document", documentID)
	case db.LevelWarn:
		r.engine.log.Warn(message, "run", r.record.ID, "document", documentID)
	default:
		r.engine.log.Info(message, "run", r.record.ID, "document", documentID)
	}
}

func (r *run) execute() error {
	settings, err := r.engine.store.Settings()
	if err != nil {
		return err
	}
	r.settings = settings
	if !settings.ZoteroConfigured() {
		return errors.New("zotero is not configured")
	}
	if !settings.RemarkableConfigured() {
		return errors.New("remarkable is not paired")
	}
	r.zc = r.engine.ZoteroClient(settings)
	r.rc = r.engine.RemarkableClient(settings)

	if err := r.loadRemarkable(); err != nil {
		return errors.Wrap(err, "list remarkable items")
	}
	candidates, err := r.discover()
	if err != nil {
		return errors.Wrap(err, "discover tagged zotero items")
	}
	r.event(0, db.LevelInfo, fmt.Sprintf("found %d tagged pdf(s) in zotero and %d item(s) on the remarkable", len(candidates), len(r.items)))
	r.push(candidates)
	if err := r.pull(); err != nil {
		return err
	}
	if err := r.untag(candidates); err != nil {
		return err
	}
	return nil
}

func (r *run) loadRemarkable() error {
	_, items, err := r.rc.ListItems(r.ctx, r.engine.store)
	if err != nil {
		return err
	}
	r.items = map[string]remarkable.Item{}
	for _, item := range items {
		r.items[item.ID] = item
	}
	folder, ok := remarkable.FindFolder(items, r.settings.RemarkableFolder, "")
	if !ok {
		entry, err := r.rc.CreateFolder(r.ctx, r.settings.RemarkableFolder, "")
		if err != nil {
			return errors.Wrapf(err, "create remarkable folder %q", r.settings.RemarkableFolder)
		}
		folder = remarkable.Item{Entry: entry, ItemType: remarkable.TypeCollection, VisibleName: r.settings.RemarkableFolder}
		r.items[folder.ID] = folder
		r.event(0, db.LevelInfo, fmt.Sprintf("created remarkable folder %q", r.settings.RemarkableFolder))
	}
	r.folder = folder
	return nil
}

type candidate struct {
	Attachment zotero.Item
	Holder     zotero.Item
}

func (c candidate) title() string {
	if c.Holder.Key != c.Attachment.Key && c.Holder.Data.Title != "" {
		return c.Holder.Data.Title
	}
	return c.Attachment.Title()
}

func (r *run) discover() ([]candidate, error) {
	tagged, err := r.zc.ItemsByTag(r.ctx, r.settings.TagSend)
	if err != nil {
		return nil, err
	}
	var out []candidate
	seen := map[string]bool{}
	for _, item := range tagged {
		switch {
		case item.Data.IsStoredPDF():
			if !seen[item.Key] {
				seen[item.Key] = true
				out = append(out, candidate{Attachment: item, Holder: item})
			}
		case item.Data.IsRegularItem():
			children, err := r.zc.Children(r.ctx, item.Key)
			if err != nil {
				r.event(0, db.LevelWarn, fmt.Sprintf("cannot list attachments of %q: %v", item.Title(), err))
				continue
			}
			for _, child := range children {
				if child.Data.IsStoredPDF() && !seen[child.Key] {
					seen[child.Key] = true
					out = append(out, candidate{Attachment: child, Holder: item})
				}
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Attachment.Key < out[j].Attachment.Key })
	return out, nil
}

func (r *run) push(candidates []candidate) {
	for _, c := range candidates {
		doc, err := r.engine.store.DocumentByZoteroKey(c.Attachment.Key)
		switch {
		case db.IsDocumentNotFound(err):
			doc, err = r.engine.store.InsertDocument(db.NewDocument{
				Filename:        c.Attachment.Data.Filename,
				Title:           c.title(),
				ZoteroKey:       c.Attachment.Key,
				ZoteroMD5:       c.Attachment.Data.MD5,
				ZoteroParentKey: c.Attachment.Data.ParentItem,
			})
			if err != nil {
				r.event(0, db.LevelError, fmt.Sprintf("record %q: %v", c.title(), err))
				continue
			}
		case err != nil:
			r.event(0, db.LevelError, fmt.Sprintf("look up %q: %v", c.title(), err))
			continue
		}
		if doc.State == db.StateRemoved || doc.State == db.StateQueued || (doc.State == db.StateError && doc.RemarkableID == "") {
			if _, present := r.items[doc.RemarkableID]; present && doc.RemarkableID != "" && doc.State != db.StateRemoved {
				continue
			}
			r.pushDocument(doc, c)
		}
	}
}

func (r *run) pushDocument(doc db.Document, c candidate) {
	pdf, info, err := r.zc.DownloadFile(r.ctx, c.Attachment.Key)
	if err != nil {
		r.fail(doc, c.Holder, errors.Wrap(err, "download pdf from zotero"))
		return
	}
	entry, err := r.rc.PutPDF(r.ctx, c.title(), pdf, remarkable.PutPDFOptions{Parent: r.folder.ID})
	if err != nil {
		r.fail(doc, c.Holder, errors.Wrap(err, "upload pdf to remarkable"))
		return
	}
	r.items[entry.ID] = remarkable.Item{Entry: entry, FileType: "pdf", ItemType: remarkable.TypeDocument, Parent: r.folder.ID, VisibleName: c.title()}
	doc.AnnotatedKey = ""
	doc.AnnotatedMD5 = ""
	doc.AnnotationCount = 0
	doc.Filename = c.Attachment.Data.Filename
	doc.LastError = ""
	doc.LastPushedAt = ts.NowPtr()
	doc.RemarkableHash = entry.Hash
	doc.RemarkableID = entry.ID
	doc.SceneSignature = ""
	doc.State = db.StateSynced
	doc.Title = c.title()
	doc.ZoteroMD5 = info.MD5
	doc.ZoteroParentKey = c.Attachment.Data.ParentItem
	if _, err := r.engine.store.UpdateDocument(doc); err != nil {
		r.event(doc.ID, db.LevelError, fmt.Sprintf("save mapping for %q: %v", doc.Title, err))
		return
	}
	if err := r.engine.store.DeleteAnnotationsForDocument(doc.ID); err != nil {
		r.event(doc.ID, db.LevelWarn, err.Error())
	}
	r.setTags(doc, c.Holder, []string{db.StateSynced}, []string{db.StateError, db.StateRemoved, db.StateAnnotated})
	r.record.Pushed++
	r.event(doc.ID, db.LevelInfo, fmt.Sprintf("sent %q to the remarkable", doc.Title))
}

func (r *run) fail(doc db.Document, holder zotero.Item, err error) {
	doc.LastError = err.Error()
	doc.State = db.StateError
	if _, saveErr := r.engine.store.UpdateDocument(doc); saveErr != nil {
		r.engine.log.Warn("save document error state", "error", saveErr)
	}
	r.event(doc.ID, db.LevelError, fmt.Sprintf("%q: %v", doc.Title, err))
	if holder.Key != "" {
		r.setTags(doc, holder, []string{db.StateError}, nil)
	}
}

func (r *run) setTags(doc db.Document, holder zotero.Item, add, remove []string) zotero.Item {
	tags := holder.Data.Tags
	for _, state := range remove {
		tags = zotero.WithoutTag(tags, r.settings.StateTag(state))
	}
	for _, state := range add {
		tags = zotero.WithTag(tags, r.settings.StateTag(state))
	}
	if zotero.TagsEqual(tags, holder.Data.Tags) {
		return holder
	}
	version, err := r.zc.SetTags(r.ctx, holder, tags)
	if zotero.IsPreconditionFailed(err) {
		fresh, fetchErr := r.zc.Item(r.ctx, holder.Key)
		if fetchErr == nil {
			return r.setTags(doc, fresh, add, remove)
		}
		err = fetchErr
	}
	if err != nil {
		r.event(doc.ID, db.LevelWarn, fmt.Sprintf("update zotero tags of %q: %v", holder.Title(), err))
		return holder
	}
	holder.Version = version
	holder.Data.Tags = tags
	return holder
}

func (r *run) holder(doc db.Document) (zotero.Item, error) {
	key := doc.ZoteroParentKey
	if key == "" {
		key = doc.ZoteroKey
	}
	return r.zc.Item(r.ctx, key)
}

func (r *run) pull() error {
	docs, err := r.engine.store.ListDocuments()
	if err != nil {
		return err
	}
	for _, doc := range docs {
		if doc.RemarkableID == "" || doc.State == db.StateRemoved || doc.State == db.StateQueued {
			continue
		}
		item, present := r.items[doc.RemarkableID]
		if !present || item.IsTrashed() {
			r.removedOnDevice(doc)
			continue
		}
		if item.Hash == doc.RemarkableHash {
			continue
		}
		if err := r.pullDocument(doc, item); err != nil {
			holder, hErr := r.holder(doc)
			if hErr != nil {
				holder = zotero.Item{}
			}
			r.fail(doc, holder, err)
		}
	}
	return nil
}

func (r *run) removedOnDevice(doc db.Document) {
	if !r.settings.UntagWhenDeleted {
		return
	}
	holder, err := r.holder(doc)
	if err != nil && !zotero.IsNotFound(err) {
		r.event(doc.ID, db.LevelWarn, fmt.Sprintf("%q was removed on the remarkable but zotero lookup failed: %v", doc.Title, err))
		return
	}
	if err == nil {
		tags := zotero.WithoutTag(holder.Data.Tags, r.settings.TagSend)
		for _, state := range []string{db.StateSynced, db.StateAnnotated, db.StateError} {
			tags = zotero.WithoutTag(tags, r.settings.StateTag(state))
		}
		tags = zotero.WithTag(tags, r.settings.StateTag(db.StateRemoved))
		if _, err := r.zc.SetTags(r.ctx, holder, tags); err != nil {
			r.event(doc.ID, db.LevelWarn, fmt.Sprintf("update zotero tags of %q: %v", holder.Title(), err))
		}
	}
	doc.State = db.StateRemoved
	doc.LastError = ""
	if _, err := r.engine.store.UpdateDocument(doc); err != nil {
		r.event(doc.ID, db.LevelError, err.Error())
		return
	}
	r.record.Removed++
	r.event(doc.ID, db.LevelInfo, fmt.Sprintf("%q was removed on the remarkable; send tag cleared in zotero", doc.Title))
}

type pulled struct {
	annotations []render.Annotation
	pdf         []byte
	scenes      []render.PageScene
	signature   string
	warnings    []string
}

func (r *run) readDocument(doc db.Document, item remarkable.Item) (*pulled, error) {
	rmDoc, err := r.rc.OpenDocument(r.ctx, item.Entry)
	if err != nil {
		return nil, err
	}
	content, err := rmDoc.Content(r.ctx)
	if err != nil {
		return nil, err
	}
	pdf, err := rmDoc.PDF(r.ctx)
	if err != nil {
		return nil, errors.Wrap(err, "fetch pdf from remarkable")
	}
	boxes, err := render.PageBoxes(pdf)
	if err != nil {
		return nil, err
	}
	device := render.DeviceByKey(r.settings.RemarkableDevice)
	out := &pulled{pdf: pdf}
	for _, page := range content.PageOrder() {
		data, ok, err := rmDoc.PageFile(r.ctx, page.ID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if page.PDFPage < 0 || page.PDFPage >= len(boxes) {
			out.warnings = append(out.warnings, fmt.Sprintf("page %d is a notebook page inserted on the tablet and was skipped", page.Index+1))
			continue
		}
		scene, err := rm.Parse(data)
		if err != nil {
			out.warnings = append(out.warnings, fmt.Sprintf("page %d could not be parsed: %v", page.Index+1, err))
			continue
		}
		for _, w := range scene.Warnings {
			out.warnings = append(out.warnings, fmt.Sprintf("page %d: %s", page.Index+1, w))
		}
		if scene.IsEmpty() {
			continue
		}
		ps := render.PageScene{Box: boxes[page.PDFPage], PageID: page.ID, PageIndex: page.PDFPage, Scene: scene}
		out.scenes = append(out.scenes, ps)
		out.annotations = append(out.annotations, render.Annotations(ps, device.DPI)...)
	}
	sigs := make([]string, 0, len(out.annotations))
	for _, a := range out.annotations {
		sigs = append(sigs, a.Key+"="+a.Signature())
	}
	sort.Strings(sigs)
	sum := sha256.Sum256([]byte(strings.Join(sigs, "\n")))
	out.signature = hex.EncodeToString(sum[:])
	return out, nil
}

func (r *run) pullDocument(doc db.Document, item remarkable.Item) error {
	result, err := r.readDocument(doc, item)
	if err != nil {
		return errors.Wrap(err, "read remarkable document")
	}
	for _, w := range result.warnings {
		r.event(doc.ID, db.LevelWarn, fmt.Sprintf("%q: %s", doc.Title, w))
	}
	if result.signature == doc.SceneSignature {
		doc.RemarkableHash = item.Hash
		_, err := r.engine.store.UpdateDocument(doc)
		return err
	}
	holder, err := r.holder(doc)
	if err != nil {
		return errors.Wrap(err, "look up zotero item")
	}
	if r.settings.WriteAnnotations {
		if err := r.syncAnnotations(doc, result.annotations); err != nil {
			return err
		}
	}
	if r.settings.AnnotatedPDF && len(result.scenes) > 0 {
		if err := r.uploadAnnotatedPDF(&doc, result); err != nil {
			return err
		}
	}
	doc.AnnotationCount = len(result.annotations)
	doc.LastError = ""
	doc.LastPulledAt = ts.NowPtr()
	doc.RemarkableHash = item.Hash
	doc.SceneSignature = result.signature
	if len(result.annotations) > 0 {
		doc.State = db.StateAnnotated
	} else {
		doc.State = db.StateSynced
	}
	if _, err := r.engine.store.UpdateDocument(doc); err != nil {
		return err
	}
	if len(result.annotations) > 0 {
		r.setTags(doc, holder, []string{db.StateAnnotated}, []string{db.StateError})
	} else {
		r.setTags(doc, holder, []string{db.StateSynced}, []string{db.StateError, db.StateAnnotated})
	}
	r.record.Pulled++
	r.event(doc.ID, db.LevelInfo, fmt.Sprintf("pulled %d annotation(s) for %q into zotero", len(result.annotations), doc.Title))
	return nil
}

func (r *run) syncAnnotations(doc db.Document, annotations []render.Annotation) error {
	existing, err := r.engine.store.ListAnnotations(doc.ID)
	if err != nil {
		return err
	}
	byKey := map[string]db.Annotation{}
	for _, a := range existing {
		byKey[a.RmKey] = a
	}
	var toCreate []render.Annotation
	wanted := map[string]bool{}
	for _, ann := range annotations {
		wanted[ann.Key] = true
		old, ok := byKey[ann.Key]
		if ok && old.Signature == ann.Signature() {
			continue
		}
		if ok {
			if err := r.zc.DeleteItem(r.ctx, old.ZoteroKey, old.ZoteroVersion); err != nil && !zotero.IsPreconditionFailed(err) {
				return errors.Wrapf(err, "delete stale annotation %s", old.ZoteroKey)
			}
			if zotero.IsPreconditionFailed(err) {
				if fresh, fErr := r.zc.Item(r.ctx, old.ZoteroKey); fErr == nil {
					if err := r.zc.DeleteItem(r.ctx, fresh.Key, fresh.Version); err != nil {
						return errors.Wrapf(err, "delete stale annotation %s", old.ZoteroKey)
					}
				}
			}
		}
		toCreate = append(toCreate, ann)
	}
	for key, old := range byKey {
		if wanted[key] {
			continue
		}
		if err := r.zc.DeleteItem(r.ctx, old.ZoteroKey, old.ZoteroVersion); err != nil && !zotero.IsPreconditionFailed(err) {
			return errors.Wrapf(err, "delete erased annotation %s", old.ZoteroKey)
		}
		if err := r.engine.store.DeleteAnnotation(doc.ID, key); err != nil {
			return err
		}
	}
	if len(toCreate) == 0 {
		return nil
	}
	items := make([]zotero.ItemData, 0, len(toCreate))
	for _, ann := range toCreate {
		items = append(items, ann.ItemData(doc.ZoteroKey))
	}
	created, err := r.zc.CreateItems(r.ctx, items)
	for i, item := range created {
		ann := toCreate[i]
		if err := r.engine.store.UpsertAnnotation(db.Annotation{
			DocumentID:    doc.ID,
			Kind:          ann.Kind,
			RmKey:         ann.Key,
			Signature:     ann.Signature(),
			ZoteroKey:     item.Key,
			ZoteroVersion: item.Version,
		}); err != nil {
			return err
		}
	}
	if err != nil {
		return errors.Wrap(err, "create zotero annotations")
	}
	return nil
}

func annotatedFilename(filename, title string) string {
	base := strings.TrimSuffix(filename, ".pdf")
	if base == "" {
		base = title
	}
	if base == "" {
		base = "document"
	}
	return base + " (reMarkable).pdf"
}

func (r *run) uploadAnnotatedPDF(doc *db.Document, result *pulled) error {
	device := render.DeviceByKey(r.settings.RemarkableDevice)
	annotated, err := render.AnnotatedPDF(result.pdf, result.scenes, device.DPI)
	if err != nil {
		return errors.Wrap(err, "render annotated pdf")
	}
	md5 := zotero.MD5Hex(annotated)
	if doc.AnnotatedKey != "" && doc.AnnotatedMD5 == md5 {
		return nil
	}
	filename := annotatedFilename(doc.Filename, doc.Title)
	if doc.AnnotatedKey != "" {
		if _, err := r.zc.Item(r.ctx, doc.AnnotatedKey); zotero.IsNotFound(err) {
			doc.AnnotatedKey = ""
			doc.AnnotatedMD5 = ""
		}
	}
	if doc.AnnotatedKey == "" {
		created, err := r.zc.CreateItems(r.ctx, []zotero.ItemData{{
			ContentType: "application/pdf",
			Filename:    filename,
			ItemType:    zotero.ItemTypeAttachment,
			LinkMode:    zotero.LinkModeImportedFile,
			ParentItem:  doc.ZoteroParentKey,
			Tags:        []zotero.Tag{},
			Title:       strings.TrimSuffix(filename, ".pdf"),
		}})
		if err != nil {
			return errors.Wrap(err, "create annotated pdf attachment")
		}
		doc.AnnotatedKey = created[0].Key
		doc.AnnotatedMD5 = ""
		if _, err := r.engine.store.UpdateDocument(*doc); err != nil {
			return err
		}
	}
	existing := doc.AnnotatedMD5
	if existing == "" {
		if item, err := r.zc.Item(r.ctx, doc.AnnotatedKey); err == nil {
			existing = item.Data.MD5
		}
	}
	_, err = r.zc.UploadFile(r.ctx, zotero.UploadRequest{
		ContentType: "application/pdf",
		Data:        annotated,
		ExistingMD5: existing,
		Filename:    filename,
		Key:         doc.AnnotatedKey,
	})
	if zotero.IsPreconditionFailed(err) {
		item, fetchErr := r.zc.Item(r.ctx, doc.AnnotatedKey)
		if fetchErr != nil {
			return errors.Wrap(fetchErr, "refresh annotated pdf attachment")
		}
		_, err = r.zc.UploadFile(r.ctx, zotero.UploadRequest{ContentType: "application/pdf", Data: annotated, ExistingMD5: item.Data.MD5, Filename: filename, Key: doc.AnnotatedKey})
	}
	if err != nil {
		return errors.Wrap(err, "upload annotated pdf")
	}
	doc.AnnotatedMD5 = md5
	return nil
}

func (r *run) untag(candidates []candidate) error {
	tagged := map[string]bool{}
	for _, c := range candidates {
		tagged[c.Attachment.Key] = true
	}
	docs, err := r.engine.store.ListDocuments()
	if err != nil {
		return err
	}
	for _, doc := range docs {
		if doc.State == db.StateRemoved || doc.State == db.StateQueued || tagged[doc.ZoteroKey] {
			continue
		}
		holder, err := r.holder(doc)
		if err != nil && !zotero.IsNotFound(err) {
			r.event(doc.ID, db.LevelWarn, fmt.Sprintf("check tags of %q: %v", doc.Title, err))
			continue
		}
		if err == nil && holder.Data.HasTag(r.settings.TagSend) && holder.Data.Deleted == 0 {
			continue
		}
		if r.settings.TrashWhenUntagged && doc.RemarkableID != "" {
			if _, present := r.items[doc.RemarkableID]; present {
				if _, err := r.rc.Trash(r.ctx, doc.RemarkableID); err != nil && !errors.Is(err, remarkable.ErrNotFound) {
					r.event(doc.ID, db.LevelError, fmt.Sprintf("move %q to the remarkable trash: %v", doc.Title, err))
					continue
				}
			}
		}
		if err == nil {
			r.setTags(doc, holder, nil, []string{db.StateSynced, db.StateAnnotated, db.StateError, db.StateRemoved})
		}
		doc.State = db.StateRemoved
		doc.LastError = ""
		if _, err := r.engine.store.UpdateDocument(doc); err != nil {
			return err
		}
		r.record.Removed++
		r.event(doc.ID, db.LevelInfo, fmt.Sprintf("%q is no longer tagged; moved to the remarkable trash", doc.Title))
	}
	return nil
}
