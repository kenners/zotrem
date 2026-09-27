package remarkabletest

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/kenners/zotrem/pkg/remarkable"
)

type Server struct {
	*httptest.Server
	DeviceToken  string
	Generation   int64
	PairingCode  string
	Requests     []string
	RootHash     string
	Schema       int
	UserToken    string
	blobs        map[string][]byte
	minted       int
	mu           sync.Mutex
	tokenRefresh int
}

type ItemState struct {
	Content  remarkable.Content
	Files    map[string][]byte
	ID       string
	Metadata remarkable.Metadata
}

func New() *Server {
	s := &Server{
		DeviceToken: "device-token-123",
		Generation:  1,
		PairingCode: "abcdefgh",
		Schema:      4,
		blobs:       map[string][]byte{},
	}
	s.UserToken = s.mintUserToken(time.Now().Add(6 * time.Hour))
	s.writeRoot(nil)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token/json/2/device/new", s.handleRegister)
	mux.HandleFunc("POST /token/json/2/user/new", s.handleUserToken)
	mux.HandleFunc("GET /sync/v4/root", s.withAuth(s.handleGetRoot))
	mux.HandleFunc("PUT /sync/v3/root", s.withAuth(s.handlePutRoot))
	mux.HandleFunc("GET /sync/v3/files/{hash}", s.withAuth(s.handleGetBlob))
	mux.HandleFunc("PUT /sync/v3/files/{hash}", s.withAuth(s.handlePutBlob))
	s.Server = httptest.NewServer(mux)
	return s
}

func (s *Server) mintUserToken(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	s.minted++
	payload := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf(`{"device-id":"dev","exp":%d,"n":%d}`, exp.Unix(), s.minted)))
	return header + "." + payload + ".sig"
}

func (s *Server) ExpireUserToken() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.UserToken = s.mintUserToken(time.Now().Add(6 * time.Hour))
}

func (s *Server) TokenRefreshes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenRefresh
}

func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.Requests = append(s.Requests, r.Method+" "+r.URL.Path)
		token := s.UserToken
		s.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Code       string `json:"code"`
		DeviceDesc string `json:"deviceDesc"`
		DeviceID   string `json:"deviceID"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code != s.PairingCode || body.DeviceID == "" || body.DeviceDesc == "" {
		http.Error(w, "invalid or expired code", http.StatusBadRequest)
		return
	}
	io.WriteString(w, s.DeviceToken)
}

func (s *Server) handleUserToken(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+s.DeviceToken {
		http.Error(w, "invalid device token", http.StatusUnauthorized)
		return
	}
	s.mu.Lock()
	s.tokenRefresh++
	token := s.UserToken
	s.mu.Unlock()
	io.WriteString(w, token)
}

func (s *Server) handleGetRoot(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeJSON(w, map[string]any{"generation": s.Generation, "hash": s.RootHash, "schemaVersion": s.Schema})
}

func (s *Server) handlePutRoot(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var body struct {
		Broadcast  bool   `json:"broadcast"`
		Generation int64  `json:"generation"`
		Hash       string `json:"hash"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if body.Generation != s.Generation {
		w.WriteHeader(http.StatusBadRequest)
		io.WriteString(w, "{\"message\":\"precondition failed\"}\n")
		return
	}
	if _, ok := s.blobs[body.Hash]; !ok {
		http.Error(w, "unknown root hash", http.StatusBadRequest)
		return
	}
	s.RootHash = body.Hash
	s.Generation++
	writeJSON(w, map[string]any{"generation": s.Generation, "hash": s.RootHash})
}

func (s *Server) handleGetBlob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("rm-filename") == "" {
		http.Error(w, "missing rm-filename", http.StatusBadRequest)
		return
	}
	data, ok := s.blobs[r.PathValue("hash")]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Write(data)
}

func (s *Server) handlePutBlob(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Header.Get("rm-filename") == "" || !strings.HasPrefix(r.Header.Get("x-goog-hash"), "crc32c=") {
		http.Error(w, "missing headers", http.StatusBadRequest)
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	if remarkable.HashBytes(data) != r.PathValue("hash") {
		http.Error(w, "hash mismatch", http.StatusBadRequest)
		return
	}
	s.blobs[r.PathValue("hash")] = data
	w.WriteHeader(http.StatusOK)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func (s *Server) putBlob(data []byte) string {
	hash := remarkable.HashBytes(data)
	s.blobs[hash] = data
	return hash
}

func (s *Server) writeRoot(entries []remarkable.Entry) {
	data, entry, err := remarkable.EncodeIndex("root", entries, 4)
	if err != nil {
		panic(err)
	}
	s.putBlob(data)
	s.RootHash = entry.Hash
	s.Generation++
}

func (s *Server) rootEntries() []remarkable.Entry {
	data, ok := s.blobs[s.RootHash]
	if !ok {
		return nil
	}
	idx, err := remarkable.ParseIndex(string(data))
	if err != nil {
		panic(err)
	}
	return idx.Entries
}

func (s *Server) writeItem(id string, files map[string][]byte) {
	entries := make([]remarkable.Entry, 0, len(files))
	for name, data := range files {
		s.putBlob(data)
		entries = append(entries, remarkable.FileEntry(name, data))
	}
	data, entry, err := remarkable.EncodeIndex(id, entries, s.Schema)
	if err != nil {
		panic(err)
	}
	s.putBlob(data)
	root := s.rootEntries()
	out := make([]remarkable.Entry, 0, len(root)+1)
	for _, e := range root {
		if e.ID != id {
			out = append(out, e)
		}
	}
	s.writeRoot(append(out, entry))
}

func (s *Server) itemFiles(id string) map[string][]byte {
	for _, e := range s.rootEntries() {
		if e.ID != id {
			continue
		}
		idx, err := remarkable.ParseIndex(string(s.blobs[e.Hash]))
		if err != nil {
			panic(err)
		}
		files := map[string][]byte{}
		for _, f := range idx.Entries {
			files[f.ID] = s.blobs[f.Hash]
		}
		return files
	}
	return nil
}

func (s *Server) AddFolder(name, parent string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := uuid.NewString()
	meta, _ := json.Marshal(remarkable.Metadata{LastModified: now(), Parent: parent, Type: remarkable.TypeCollection, VisibleName: name})
	s.writeItem(id, map[string][]byte{id + ".content": []byte(`{"tags":[]}`), id + ".metadata": meta})
	return id
}

func (s *Server) AddPDF(name, parent string, pdf []byte, pageCount int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := uuid.NewString()
	meta, _ := json.Marshal(remarkable.Metadata{LastModified: now(), Parent: parent, Type: remarkable.TypeDocument, VisibleName: name})
	content := map[string]any{
		"cPages":    map[string]any{"pages": []any{}},
		"fileType":  "pdf",
		"pageCount": pageCount,
		"zoomMode":  "bestFit",
	}
	pages := make([]any, 0, pageCount)
	for i := range pageCount {
		pages = append(pages, map[string]any{
			"id":    uuid.NewString(),
			"idx":   map[string]any{"timestamp": "1:2", "value": fmt.Sprintf("b%c", 'a'+i)},
			"redir": map[string]any{"timestamp": "1:2", "value": i},
		})
	}
	content["cPages"] = map[string]any{"pages": pages}
	contentData, _ := json.Marshal(content)
	s.writeItem(id, map[string][]byte{
		id + ".content":  contentData,
		id + ".metadata": meta,
		id + ".pagedata": []byte("\n"),
		id + ".pdf":      pdf,
	})
	return id
}

func (s *Server) SetPageStrokes(id string, pdfPage int, rm []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files := s.itemFiles(id)
	var content remarkable.Content
	if err := json.Unmarshal(files[id+".content"], &content); err != nil {
		panic(err)
	}
	for _, p := range content.PageOrder() {
		if p.PDFPage == pdfPage {
			files[id+"/"+p.ID+".rm"] = rm
		}
	}
	s.writeItem(id, files)
}

func (s *Server) Touch(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	files := s.itemFiles(id)
	var meta remarkable.Metadata
	if err := json.Unmarshal(files[id+".metadata"], &meta); err != nil {
		panic(err)
	}
	meta.LastOpened = now()
	meta.LastModified = now()
	files[id+".metadata"], _ = json.Marshal(meta)
	s.writeItem(id, files)
}

func (s *Server) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []remarkable.Entry
	for _, e := range s.rootEntries() {
		if e.ID != id {
			out = append(out, e)
		}
	}
	s.writeRoot(out)
}

func (s *Server) Items() map[string]ItemState {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]ItemState{}
	for _, e := range s.rootEntries() {
		files := s.itemFiles(e.ID)
		state := ItemState{Files: files, ID: e.ID}
		json.Unmarshal(files[e.ID+".metadata"], &state.Metadata)
		json.Unmarshal(files[e.ID+".content"], &state.Content)
		out[e.ID] = state
	}
	return out
}

func (s *Server) Item(id string) (ItemState, bool) {
	item, ok := s.Items()[id]
	return item, ok
}

func (s *Server) ChildrenOf(parent string) []ItemState {
	var out []ItemState
	for _, item := range s.Items() {
		if item.Metadata.Parent == parent {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Metadata.VisibleName < out[j].Metadata.VisibleName })
	return out
}

func now() string { return strconv.FormatInt(time.Now().UnixMilli(), 10) }
