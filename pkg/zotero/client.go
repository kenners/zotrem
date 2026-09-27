package zotero

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

const DefaultBaseURL = "https://api.zotero.org"

const maxWriteItems = 50

type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
	library string
	log     *slog.Logger
	retries int
	sleep   func(time.Duration)
}

type Option func(*Client)

type APIError struct {
	Body   string
	Method string
	Path   string
	Status int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("zotero %s %s: %d %s", e.Method, e.Path, e.Status, strings.TrimSpace(e.Body))
}

func WithBaseURL(base string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(base, "/") }
}

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func WithLibrary(libraryType string, id int64) Option {
	return func(c *Client) {
		prefix := "users"
		if libraryType == "group" {
			prefix = "groups"
		}
		c.library = fmt.Sprintf("/%s/%d", prefix, id)
	}
}

func WithLogger(log *slog.Logger) Option { return func(c *Client) { c.log = log } }

func WithSleep(sleep func(time.Duration)) Option { return func(c *Client) { c.sleep = sleep } }

func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		apiKey:  apiKey,
		baseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: 5 * time.Minute},
		log:     slog.Default(),
		retries: 3,
		sleep:   time.Sleep,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) HasLibrary() bool { return c.library != "" }

func StatusOf(err error) int {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status
	}
	return 0
}

func IsNotFound(err error) bool { return StatusOf(err) == http.StatusNotFound }

func IsPreconditionFailed(err error) bool { return StatusOf(err) == http.StatusPreconditionFailed }

type response struct {
	Body    []byte
	Header  http.Header
	Status  int
	Version int
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, headers http.Header, body []byte) (*response, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
		if err != nil {
			return nil, errors.Wrap(err, "build request")
		}
		req.Header.Set("Zotero-API-Version", "3")
		if c.apiKey != "" {
			req.Header.Set("Zotero-API-Key", c.apiKey)
		}
		for k, vs := range headers {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = errors.Wrapf(err, "%s %s", method, path)
			if attempt < c.retries {
				c.sleep(backoffDelay(attempt))
				continue
			}
			return nil, lastErr
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, errors.Wrap(readErr, "read response")
		}
		if backoff := resp.Header.Get("Backoff"); backoff != "" {
			if secs, err := strconv.Atoi(backoff); err == nil && secs > 0 {
				c.log.Warn("zotero asked for backoff", "seconds", secs)
				c.sleep(time.Duration(min(secs, 120)) * time.Second)
			}
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			lastErr = &APIError{Body: string(data), Method: method, Path: path, Status: resp.StatusCode}
			if attempt < c.retries {
				c.sleep(retryAfter(resp.Header, attempt))
				continue
			}
			return nil, lastErr
		}
		out := &response{Body: data, Header: resp.Header, Status: resp.StatusCode}
		if v, err := strconv.Atoi(resp.Header.Get("Last-Modified-Version")); err == nil {
			out.Version = v
		}
		if resp.StatusCode >= 400 {
			return out, &APIError{Body: string(data), Method: method, Path: path, Status: resp.StatusCode}
		}
		return out, nil
	}
	return nil, lastErr
}

func backoffDelay(attempt int) time.Duration {
	return time.Duration(1<<attempt) * time.Second
}

func retryAfter(h http.Header, attempt int) time.Duration {
	if secs, err := strconv.Atoi(h.Get("Retry-After")); err == nil && secs > 0 {
		return time.Duration(min(secs, 120)) * time.Second
	}
	return backoffDelay(attempt)
}

func (c *Client) getJSON(ctx context.Context, path string, query url.Values, out any) (*response, error) {
	resp, err := c.do(ctx, http.MethodGet, path, query, nil, nil)
	if err != nil {
		return resp, err
	}
	if out != nil {
		if err := json.Unmarshal(resp.Body, out); err != nil {
			return resp, errors.Wrapf(err, "decode %s", path)
		}
	}
	return resp, nil
}

func (c *Client) libraryPath(suffix string) (string, error) {
	if c.library == "" {
		return "", errors.New("zotero library not configured")
	}
	return c.library + suffix, nil
}

type KeyInfo struct {
	Access struct {
		Groups map[string]struct {
			Library bool `json:"library"`
			Write   bool `json:"write"`
		} `json:"groups"`
		User struct {
			Files   bool `json:"files"`
			Library bool `json:"library"`
			Notes   bool `json:"notes"`
			Write   bool `json:"write"`
		} `json:"user"`
	} `json:"access"`
	Key      string `json:"key"`
	UserID   int64  `json:"userID"`
	Username string `json:"username"`
}

func (c *Client) CurrentKey(ctx context.Context) (KeyInfo, error) {
	var info KeyInfo
	if _, err := c.getJSON(ctx, "/keys/current", nil, &info); err != nil {
		return KeyInfo{}, err
	}
	return info, nil
}

func (c *Client) listItems(ctx context.Context, path string, query url.Values) ([]Item, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("limit", "100")
	query.Set("format", "json")
	var all []Item
	start := 0
	for {
		query.Set("start", strconv.Itoa(start))
		var page []Item
		resp, err := c.getJSON(ctx, path, query, &page)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		total, _ := strconv.Atoi(resp.Header.Get("Total-Results"))
		start += len(page)
		if len(page) == 0 || start >= total {
			return all, nil
		}
	}
}

func (c *Client) ItemsByTag(ctx context.Context, tag string) ([]Item, error) {
	path, err := c.libraryPath("/items")
	if err != nil {
		return nil, err
	}
	return c.listItems(ctx, path, url.Values{"tag": {tag}})
}

func (c *Client) ItemsByKeys(ctx context.Context, keys []string) ([]Item, error) {
	path, err := c.libraryPath("/items")
	if err != nil {
		return nil, err
	}
	var all []Item
	for i := 0; i < len(keys); i += 50 {
		chunk := keys[i:min(i+50, len(keys))]
		items, err := c.listItems(ctx, path, url.Values{"itemKey": {strings.Join(chunk, ",")}, "includeTrashed": {"1"}})
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	return all, nil
}

func (c *Client) Item(ctx context.Context, key string) (Item, error) {
	path, err := c.libraryPath("/items/" + key)
	if err != nil {
		return Item{}, err
	}
	var item Item
	if _, err := c.getJSON(ctx, path, nil, &item); err != nil {
		return Item{}, err
	}
	return item, nil
}

func (c *Client) Children(ctx context.Context, key string) ([]Item, error) {
	path, err := c.libraryPath("/items/" + key + "/children")
	if err != nil {
		return nil, err
	}
	return c.listItems(ctx, path, nil)
}

type WriteResult struct {
	Failed     map[string]WriteFailure `json:"failed"`
	Successful map[string]Item         `json:"successful"`
	Unchanged  map[string]string       `json:"unchanged"`
}

type WriteFailure struct {
	Code    int    `json:"code"`
	Key     string `json:"key"`
	Message string `json:"message"`
}

func (c *Client) CreateItems(ctx context.Context, items []ItemData) ([]Item, error) {
	path, err := c.libraryPath("/items")
	if err != nil {
		return nil, err
	}
	var created []Item
	for i := 0; i < len(items); i += maxWriteItems {
		chunk := items[i:min(i+maxWriteItems, len(items))]
		body, err := json.Marshal(chunk)
		if err != nil {
			return nil, errors.Wrap(err, "encode items")
		}
		resp, err := c.do(ctx, http.MethodPost, path, nil, http.Header{"Content-Type": {"application/json"}}, body)
		if err != nil {
			return nil, err
		}
		var result WriteResult
		if err := json.Unmarshal(resp.Body, &result); err != nil {
			return nil, errors.Wrap(err, "decode write result")
		}
		for idx := range chunk {
			key := strconv.Itoa(idx)
			if item, ok := result.Successful[key]; ok {
				created = append(created, item)
				continue
			}
			if failure, ok := result.Failed[key]; ok {
				return created, errors.Newf("zotero rejected item %d: %d %s", i+idx, failure.Code, failure.Message)
			}
			return created, errors.Newf("zotero returned no result for item %d", i+idx)
		}
	}
	return created, nil
}

func (c *Client) PatchItem(ctx context.Context, key string, version int, patch map[string]any) (int, error) {
	path, err := c.libraryPath("/items/" + key)
	if err != nil {
		return 0, err
	}
	body, err := json.Marshal(patch)
	if err != nil {
		return 0, errors.Wrap(err, "encode patch")
	}
	headers := http.Header{
		"Content-Type":                {"application/json"},
		"If-Unmodified-Since-Version": {strconv.Itoa(version)},
	}
	resp, err := c.do(ctx, http.MethodPatch, path, nil, headers, body)
	if err != nil {
		return 0, err
	}
	return resp.Version, nil
}

func (c *Client) DeleteItem(ctx context.Context, key string, version int) error {
	path, err := c.libraryPath("/items/" + key)
	if err != nil {
		return err
	}
	headers := http.Header{"If-Unmodified-Since-Version": {strconv.Itoa(version)}}
	_, err = c.do(ctx, http.MethodDelete, path, nil, headers, nil)
	if IsNotFound(err) {
		return nil
	}
	return err
}

func (c *Client) SetTags(ctx context.Context, item Item, tags []Tag) (int, error) {
	return c.PatchItem(ctx, item.Key, item.Version, map[string]any{"tags": tags})
}

func (c *Client) ItemURL(key string) string {
	if c.library == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(c.library, "/"), "/")
	if len(parts) != 2 {
		return ""
	}
	if parts[0] == "groups" {
		return "https://www.zotero.org/groups/" + parts[1] + "/items/" + key
	}
	return "zotero://select/library/items/" + key
}
