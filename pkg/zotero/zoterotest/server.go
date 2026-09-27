package zoterotest

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const boundary = "zoterotestboundary"

type Item struct {
	Data    map[string]any
	Key     string
	Version int
}

type Server struct {
	*httptest.Server
	APIKey         string
	Files          map[string][]byte
	Items          map[string]*Item
	LibraryVersion int
	PageSize       int
	Requests       []string
	UserID         int64
	failures       []int
	mu             sync.Mutex
	nextKey        int
	uploads        map[string]pendingUpload
}

type pendingUpload struct {
	Filename string
	ItemKey  string
	MD5      string
	Mtime    int64
}

func New() *Server {
	s := &Server{
		APIKey:   "test-api-key",
		Files:    map[string][]byte{},
		Items:    map[string]*Item{},
		PageSize: 100,
		UserID:   12345,
		uploads:  map[string]pendingUpload{},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /keys/current", s.withAuth(s.handleKey))
	mux.HandleFunc("GET /users/{uid}/items", s.withAuth(s.handleListItems))
	mux.HandleFunc("POST /users/{uid}/items", s.withAuth(s.handleCreateItems))
	mux.HandleFunc("GET /users/{uid}/items/{key}", s.withAuth(s.handleGetItem))
	mux.HandleFunc("PATCH /users/{uid}/items/{key}", s.withAuth(s.handlePatchItem))
	mux.HandleFunc("DELETE /users/{uid}/items/{key}", s.withAuth(s.handleDeleteItem))
	mux.HandleFunc("GET /users/{uid}/items/{key}/children", s.withAuth(s.handleChildren))
	mux.HandleFunc("GET /users/{uid}/items/{key}/file", s.withAuth(s.handleGetFile))
	mux.HandleFunc("POST /users/{uid}/items/{key}/file", s.withAuth(s.handlePostFile))
	mux.HandleFunc("GET /storage/{key}", s.handleStorageGet)
	mux.HandleFunc("POST /s3upload", s.handleStoragePost)
	s.Server = httptest.NewServer(mux)
	return s
}

func (s *Server) FailNext(statuses ...int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failures = append(s.failures, statuses...)
}

func (s *Server) AddItem(data map[string]any) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addItemLocked(data)
}

func (s *Server) addItemLocked(data map[string]any) *Item {
	s.nextKey++
	key := fmt.Sprintf("KEY%05d", s.nextKey)
	if k, ok := data["key"].(string); ok && k != "" {
		key = k
	}
	s.LibraryVersion++
	copied := map[string]any{}
	for k, v := range data {
		copied[k] = v
	}
	copied["key"] = key
	copied["version"] = s.LibraryVersion
	if _, ok := copied["tags"]; !ok {
		copied["tags"] = []any{}
	}
	item := &Item{Data: copied, Key: key, Version: s.LibraryVersion}
	s.Items[key] = item
	return item
}

func (s *Server) AddPaper(title string, tags ...string) *Item {
	tagList := make([]any, 0, len(tags))
	for _, t := range tags {
		tagList = append(tagList, map[string]any{"tag": t})
	}
	return s.AddItem(map[string]any{"itemType": "journalArticle", "tags": tagList, "title": title})
}

func (s *Server) AddPDF(parentKey, filename string, data []byte) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	sum := md5.Sum(data)
	item := s.addItemLocked(map[string]any{
		"contentType": "application/pdf",
		"filename":    filename,
		"itemType":    "attachment",
		"linkMode":    "imported_file",
		"md5":         hex.EncodeToString(sum[:]),
		"mtime":       int64(1700000000000),
		"parentItem":  parentKey,
		"title":       filename,
	})
	s.Files[item.Key] = data
	return item
}

func (s *Server) Item(key string) *Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Items[key]
}

func (s *Server) Tags(key string) []string {
	item := s.Item(key)
	if item == nil {
		return nil
	}
	return tagNames(item.Data)
}

func (s *Server) ChildrenOf(key string) []*Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*Item
	for _, item := range s.Items {
		if item.Data["parentItem"] == key {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (s *Server) SetTags(key string, tags ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.Items[key]
	tagList := make([]any, 0, len(tags))
	for _, t := range tags {
		tagList = append(tagList, map[string]any{"tag": t})
	}
	item.Data["tags"] = tagList
	s.bumpLocked(item)
}

func (s *Server) bumpLocked(item *Item) {
	s.LibraryVersion++
	item.Version = s.LibraryVersion
	item.Data["version"] = s.LibraryVersion
}

func tagNames(data map[string]any) []string {
	var out []string
	if tags, ok := data["tags"].([]any); ok {
		for _, t := range tags {
			if m, ok := t.(map[string]any); ok {
				if name, ok := m["tag"].(string); ok {
					out = append(out, name)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func hasTag(data map[string]any, tag string) bool {
	for _, t := range tagNames(data) {
		if strings.EqualFold(t, tag) {
			return true
		}
	}
	return false
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.Requests = append(s.Requests, r.Method+" "+r.URL.RequestURI())
		if len(s.failures) > 0 {
			status := s.failures[0]
			s.failures = s.failures[1:]
			s.mu.Unlock()
			if status == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "0")
			}
			http.Error(w, "injected failure", status)
			return
		}
		s.mu.Unlock()
		if r.Header.Get("Zotero-API-Key") != s.APIKey {
			http.Error(w, "Invalid key", http.StatusForbidden)
			return
		}
		if uid := r.PathValue("uid"); uid != "" && uid != strconv.FormatInt(s.UserID, 10) {
			http.Error(w, "unknown user", http.StatusNotFound)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleKey(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"access": map[string]any{
			"groups": map[string]any{},
			"user":   map[string]any{"files": true, "library": true, "notes": true, "write": true},
		},
		"key":      s.APIKey,
		"userID":   s.UserID,
		"username": "tester",
	})
}

func (s *Server) itemJSON(item *Item) map[string]any {
	return map[string]any{
		"data":    item.Data,
		"key":     item.Key,
		"meta":    map[string]any{},
		"version": item.Version,
	}
}

func (s *Server) sortedItems() []*Item {
	out := make([]*Item, 0, len(s.Items))
	for _, item := range s.Items {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func (s *Server) handleListItems(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q := r.URL.Query()
	var keys map[string]bool
	if raw := q.Get("itemKey"); raw != "" {
		keys = map[string]bool{}
		for _, k := range strings.Split(raw, ",") {
			keys[k] = true
		}
	}
	includeTrashed := q.Get("includeTrashed") == "1"
	var matched []*Item
	for _, item := range s.sortedItems() {
		if keys != nil && !keys[item.Key] {
			continue
		}
		if !includeTrashed && item.Data["deleted"] != nil {
			continue
		}
		if !matchesTags(item.Data, q["tag"]) {
			continue
		}
		if typ := q.Get("itemType"); typ != "" && item.Data["itemType"] != typ {
			continue
		}
		matched = append(matched, item)
	}
	s.writePage(w, r, matched)
}

func matchesTags(data map[string]any, filters []string) bool {
	for _, filter := range filters {
		negate := strings.HasPrefix(filter, "-")
		filter = strings.TrimPrefix(filter, "-")
		any := false
		for _, alt := range strings.Split(filter, " || ") {
			if hasTag(data, strings.TrimSpace(alt)) {
				any = true
			}
		}
		if any == negate {
			return false
		}
	}
	return true
}

func (s *Server) writePage(w http.ResponseWriter, r *http.Request, items []*Item) {
	q := r.URL.Query()
	start, _ := strconv.Atoi(q.Get("start"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 || limit > s.PageSize {
		limit = s.PageSize
	}
	total := len(items)
	end := min(start+limit, total)
	if start > total {
		start = total
	}
	page := make([]map[string]any, 0, end-start)
	for _, item := range items[start:end] {
		page = append(page, s.itemJSON(item))
	}
	w.Header().Set("Total-Results", strconv.Itoa(total))
	w.Header().Set("Last-Modified-Version", strconv.Itoa(s.LibraryVersion))
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) handleGetItem(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.Items[r.PathValue("key")]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Last-Modified-Version", strconv.Itoa(item.Version))
	writeJSON(w, http.StatusOK, s.itemJSON(item))
}

func (s *Server) handleChildren(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.PathValue("key")
	if _, ok := s.Items[key]; !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	var matched []*Item
	for _, item := range s.sortedItems() {
		if item.Data["parentItem"] == key && item.Data["deleted"] == nil {
			matched = append(matched, item)
		}
	}
	s.writePage(w, r, matched)
}

func (s *Server) handleCreateItems(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var incoming []map[string]any
	if err := json.NewDecoder(r.Body).Decode(&incoming); err != nil {
		http.Error(w, "Uploaded data must be a JSON array", http.StatusBadRequest)
		return
	}
	if len(incoming) > 50 {
		http.Error(w, "Cannot add more than 50 items at a time", http.StatusRequestEntityTooLarge)
		return
	}
	successful := map[string]any{}
	failed := map[string]any{}
	for idx, data := range incoming {
		index := strconv.Itoa(idx)
		if msg := s.validateLocked(data); msg != "" {
			failed[index] = map[string]any{"code": 400, "message": msg}
			continue
		}
		item := s.addItemLocked(data)
		successful[index] = s.itemJSON(item)
	}
	w.Header().Set("Last-Modified-Version", strconv.Itoa(s.LibraryVersion))
	writeJSON(w, http.StatusOK, map[string]any{"failed": failed, "successful": successful, "unchanged": map[string]any{}})
}

func (s *Server) validateLocked(data map[string]any) string {
	itemType, _ := data["itemType"].(string)
	if itemType == "" {
		return "'itemType' property not provided"
	}
	parentKey, _ := data["parentItem"].(string)
	if parentKey != "" {
		parent, ok := s.Items[parentKey]
		if !ok {
			return "Parent item " + parentKey + " not found"
		}
		if itemType == "annotation" && parent.Data["itemType"] != "attachment" {
			return "Parent item of annotation must be an attachment"
		}
	}
	if itemType == "annotation" {
		if parentKey == "" {
			return "Annotation must have a parent item"
		}
		if color, _ := data["annotationColor"].(string); color != "" && (len(color) != 7 || color[0] != '#') {
			return "annotationColor must be a hex color (e.g., '#FF0000')"
		}
		if _, ok := data["annotationPosition"].(string); !ok {
			return "annotationPosition must be a JSON string"
		}
		if _, ok := data["annotationSortIndex"].(string); !ok {
			return "annotationSortIndex must be provided"
		}
	}
	return ""
}

func (s *Server) handlePatchItem(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.Items[r.PathValue("key")]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if !s.checkVersion(w, r, item) {
		return
	}
	var patch map[string]any
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	for k, v := range patch {
		if k == "key" || k == "version" {
			continue
		}
		item.Data[k] = v
	}
	s.bumpLocked(item)
	w.Header().Set("Last-Modified-Version", strconv.Itoa(item.Version))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) checkVersion(w http.ResponseWriter, r *http.Request, item *Item) bool {
	raw := r.Header.Get("If-Unmodified-Since-Version")
	if raw == "" {
		http.Error(w, "If-Unmodified-Since-Version must be provided", http.StatusPreconditionRequired)
		return false
	}
	version, _ := strconv.Atoi(raw)
	if version != item.Version {
		w.Header().Set("Last-Modified-Version", strconv.Itoa(item.Version))
		http.Error(w, fmt.Sprintf("Item has been modified since specified version (expected %d, found %d)", version, item.Version), http.StatusPreconditionFailed)
		return false
	}
	return true
}

func (s *Server) handleDeleteItem(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.Items[r.PathValue("key")]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if !s.checkVersion(w, r, item) {
		return
	}
	delete(s.Items, item.Key)
	delete(s.Files, item.Key)
	s.LibraryVersion++
	w.Header().Set("Last-Modified-Version", strconv.Itoa(s.LibraryVersion))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleGetFile(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.PathValue("key")
	item, ok := s.Items[key]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if item.Data["itemType"] != "attachment" {
		http.Error(w, "Item is not an attachment", http.StatusBadRequest)
		return
	}
	data, ok := s.Files[key]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	sum := md5.Sum(data)
	w.Header().Set("Zotero-File-MD5", hex.EncodeToString(sum[:]))
	w.Header().Set("Zotero-File-Size", strconv.Itoa(len(data)))
	w.Header().Set("Zotero-File-Modification-Time", fmt.Sprint(item.Data["mtime"]))
	w.Header().Set("Location", s.URL+"/storage/"+key+"?signature=abc")
	w.WriteHeader(http.StatusFound)
}

func (s *Server) handleStorageGet(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("Zotero-API-Key") != "" {
		http.Error(w, "api key leaked to storage", http.StatusBadRequest)
		return
	}
	data, ok := s.Files[r.PathValue("key")]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Write(data)
}

func (s *Server) handlePostFile(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := r.PathValue("key")
	item, ok := s.Items[key]
	if !ok {
		http.Error(w, "Not found", http.StatusNotFound)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	currentMD5, _ := item.Data["md5"].(string)
	_, hasFile := s.Files[key]
	ifMatch := r.Header.Get("If-Match")
	ifNoneMatch := r.Header.Get("If-None-Match")
	switch {
	case ifMatch == "" && ifNoneMatch == "":
		http.Error(w, "If-Match/If-None-Match header not provided", http.StatusPreconditionRequired)
		return
	case ifNoneMatch != "" && ifNoneMatch != "*":
		http.Error(w, "Invalid value for If-None-Match header", http.StatusBadRequest)
		return
	case ifNoneMatch == "*" && hasFile:
		http.Error(w, "If-None-Match: * set but file exists", http.StatusPreconditionFailed)
		return
	case ifMatch != "" && !hasFile:
		http.Error(w, "If-Match set but file does not exist", http.StatusPreconditionFailed)
		return
	case ifMatch != "" && ifMatch != currentMD5:
		http.Error(w, "ETag does not match current version of file", http.StatusPreconditionFailed)
		return
	}
	if uploadKey := r.Form.Get("upload"); uploadKey != "" {
		pending, ok := s.uploads[uploadKey]
		if !ok || pending.ItemKey != key {
			http.Error(w, "Upload key not found", http.StatusBadRequest)
			return
		}
		data, ok := s.Files["pending:"+uploadKey]
		if !ok {
			http.Error(w, "Remote file not found", http.StatusBadRequest)
			return
		}
		delete(s.Files, "pending:"+uploadKey)
		delete(s.uploads, uploadKey)
		s.Files[key] = data
		item.Data["filename"] = pending.Filename
		item.Data["md5"] = pending.MD5
		item.Data["mtime"] = pending.Mtime
		s.bumpLocked(item)
		w.Header().Set("Last-Modified-Version", strconv.Itoa(item.Version))
		w.WriteHeader(http.StatusNoContent)
		return
	}
	for _, field := range []string{"md5", "filename", "filesize", "mtime"} {
		if r.Form.Get(field) == "" {
			http.Error(w, field+" not provided", http.StatusBadRequest)
			return
		}
	}
	sum := r.Form.Get("md5")
	for existingKey, data := range s.Files {
		existing := md5.Sum(data)
		if hex.EncodeToString(existing[:]) == sum && existingKey != key && !strings.HasPrefix(existingKey, "pending:") {
			s.Files[key] = data
			item.Data["filename"] = r.Form.Get("filename")
			item.Data["md5"] = sum
			item.Data["mtime"], _ = strconv.ParseInt(r.Form.Get("mtime"), 10, 64)
			s.bumpLocked(item)
			w.Header().Set("Last-Modified-Version", strconv.Itoa(item.Version))
			writeJSON(w, http.StatusOK, map[string]any{"exists": 1})
			return
		}
	}
	uploadKey := fmt.Sprintf("upload%d", len(s.uploads)+1+s.LibraryVersion)
	mtime, _ := strconv.ParseInt(r.Form.Get("mtime"), 10, 64)
	s.uploads[uploadKey] = pendingUpload{Filename: r.Form.Get("filename"), ItemKey: key, MD5: sum, Mtime: mtime}
	writeJSON(w, http.StatusOK, map[string]any{
		"contentType": "multipart/form-data; boundary=" + boundary,
		"prefix":      "--" + boundary + "\r\nContent-Disposition: form-data; name=\"key\"\r\n\r\n" + uploadKey + "\r\n--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"\r\n\r\n",
		"suffix":      "\r\n--" + boundary + "--",
		"uploadKey":   uploadKey,
		"url":         s.URL + "/s3upload?upload=" + uploadKey,
	})
}

func (s *Server) handleStoragePost(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uploadKey := r.URL.Query().Get("upload")
	pending, ok := s.uploads[uploadKey]
	if !ok {
		http.Error(w, "unknown upload", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	prefix := "--" + boundary + "\r\nContent-Disposition: form-data; name=\"key\"\r\n\r\n" + uploadKey + "\r\n--" + boundary + "\r\nContent-Disposition: form-data; name=\"file\"\r\n\r\n"
	suffix := "\r\n--" + boundary + "--"
	if !strings.HasPrefix(string(body), prefix) || !strings.HasSuffix(string(body), suffix) {
		http.Error(w, "malformed multipart body", http.StatusBadRequest)
		return
	}
	data := body[len(prefix) : len(body)-len(suffix)]
	sum := md5.Sum(data)
	if hex.EncodeToString(sum[:]) != pending.MD5 {
		http.Error(w, "The Content-MD5 you specified did not match what we received.", http.StatusBadRequest)
		return
	}
	s.Files["pending:"+uploadKey] = data
	w.WriteHeader(http.StatusCreated)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
