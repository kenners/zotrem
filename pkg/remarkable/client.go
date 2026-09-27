package remarkable

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/google/uuid"
)

const (
	DefaultAuthHost = "https://webapp-prod.cloud.remarkable.engineering"
	DefaultSyncHost = "https://eu.tectonic.remarkable.com"
	PairingURL      = "https://my.remarkable.com/device/browser/connect"
	TrashID         = "trash"
)

var (
	ErrGenerationConflict = errors.New("remarkable root generation conflict")
	ErrNotFound           = errors.New("remarkable item not found")
	ErrUnauthorized       = errors.New("remarkable device token rejected")
)

var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

type APIError struct {
	Body   string
	Method string
	Path   string
	Status int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("remarkable %s %s: %d %s", e.Method, e.Path, e.Status, strings.TrimSpace(e.Body))
}

type Client struct {
	authHost    string
	deviceToken string
	http        *http.Client
	log         *slog.Logger
	mu          sync.Mutex
	onUserToken func(string)
	sleep       func(time.Duration)
	syncHost    string
	userToken   string
}

type Option func(*Client)

func WithAuthHost(host string) Option {
	return func(c *Client) { c.authHost = strings.TrimRight(host, "/") }
}

func WithSyncHost(host string) Option {
	return func(c *Client) { c.syncHost = strings.TrimRight(host, "/") }
}

func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func WithLogger(log *slog.Logger) Option { return func(c *Client) { c.log = log } }

func WithSleep(sleep func(time.Duration)) Option { return func(c *Client) { c.sleep = sleep } }

func WithUserToken(token string, onRefresh func(string)) Option {
	return func(c *Client) {
		c.userToken = token
		c.onUserToken = onRefresh
	}
}

func New(deviceToken string, opts ...Option) *Client {
	c := &Client{
		authHost:    DefaultAuthHost,
		deviceToken: deviceToken,
		http:        &http.Client{Timeout: 5 * time.Minute},
		log:         slog.Default(),
		sleep:       time.Sleep,
		syncHost:    DefaultSyncHost,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func Register(ctx context.Context, code string, opts ...Option) (string, error) {
	c := New("", opts...)
	code = strings.TrimSpace(code)
	if len(code) != 8 {
		return "", errors.New("the one-time code must be 8 characters")
	}
	body, err := json.Marshal(map[string]string{
		"code":       code,
		"deviceDesc": "browser-chrome",
		"deviceID":   uuid.NewString(),
	})
	if err != nil {
		return "", errors.Wrap(err, "encode registration")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authHost+"/token/json/2/device/new", bytes.NewReader(body))
	if err != nil {
		return "", errors.Wrap(err, "build registration request")
	}
	req.Header.Set("Authorization", "Bearer")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "register device")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Body: string(data), Method: http.MethodPost, Path: "/token/json/2/device/new", Status: resp.StatusCode}
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", errors.New("registration returned an empty device token")
	}
	return token, nil
}

func (c *Client) RefreshUserToken(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.authHost+"/token/json/2/user/new", nil)
	if err != nil {
		return "", errors.Wrap(err, "build token request")
	}
	req.Header.Set("Authorization", "Bearer "+c.deviceToken)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", errors.Wrap(err, "refresh user token")
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", errors.Wrapf(ErrUnauthorized, "%d %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if resp.StatusCode != http.StatusOK {
		return "", &APIError{Body: string(data), Method: http.MethodPost, Path: "/token/json/2/user/new", Status: resp.StatusCode}
	}
	token := strings.TrimSpace(string(data))
	c.mu.Lock()
	c.userToken = token
	c.mu.Unlock()
	if c.onUserToken != nil {
		c.onUserToken(token)
	}
	return token, nil
}

func (c *Client) currentUserToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.userToken
}

func tokenExpired(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Exp == 0 {
		return false
	}
	return time.Now().Add(5 * time.Minute).After(time.Unix(claims.Exp, 0))
}

func (c *Client) ensureUserToken(ctx context.Context) (string, error) {
	token := c.currentUserToken()
	if token != "" && !tokenExpired(token) {
		return token, nil
	}
	return c.RefreshUserToken(ctx)
}

func (c *Client) do(ctx context.Context, method, path string, headers http.Header, body []byte) ([]byte, int, error) {
	refreshed := false
	for attempt := 0; ; attempt++ {
		token, err := c.ensureUserToken(ctx)
		if err != nil {
			return nil, 0, err
		}
		req, err := http.NewRequestWithContext(ctx, method, c.syncHost+path, bytes.NewReader(body))
		if err != nil {
			return nil, 0, errors.Wrap(err, "build request")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		for k, vs := range headers {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if attempt < 3 {
				c.sleep(time.Duration(1<<attempt) * time.Second)
				continue
			}
			return nil, 0, errors.Wrapf(err, "%s %s", method, path)
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, resp.StatusCode, errors.Wrap(readErr, "read response")
		}
		switch {
		case resp.StatusCode == http.StatusUnauthorized && !refreshed:
			refreshed = true
			if _, err := c.RefreshUserToken(ctx); err != nil {
				return nil, resp.StatusCode, err
			}
			continue
		case (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < 3:
			c.sleep(time.Duration(1<<attempt) * time.Second)
			continue
		case resp.StatusCode >= 200 && resp.StatusCode < 300:
			return data, resp.StatusCode, nil
		case strings.Contains(string(data), "precondition failed"):
			return data, resp.StatusCode, ErrGenerationConflict
		case resp.StatusCode == http.StatusNotFound:
			return data, resp.StatusCode, errors.Wrapf(ErrNotFound, "%s %s", method, path)
		default:
			return data, resp.StatusCode, &APIError{Body: string(data), Method: method, Path: path, Status: resp.StatusCode}
		}
	}
}

type Root struct {
	Generation    int64  `json:"generation"`
	Hash          string `json:"hash"`
	SchemaVersion int    `json:"schemaVersion"`
}

func (c *Client) Root(ctx context.Context) (Root, error) {
	data, _, err := c.do(ctx, http.MethodGet, "/sync/v4/root", nil, nil)
	if err != nil {
		return Root{}, err
	}
	var root Root
	if err := json.Unmarshal(data, &root); err != nil {
		return Root{}, errors.Wrap(err, "decode root")
	}
	if root.SchemaVersion != 3 && root.SchemaVersion != 4 {
		return Root{}, errors.Newf("unsupported remarkable schema version %d", root.SchemaVersion)
	}
	return root, nil
}

func (c *Client) PutRoot(ctx context.Context, hash string, generation int64) (Root, error) {
	body, err := json.Marshal(map[string]any{"broadcast": true, "generation": generation, "hash": hash})
	if err != nil {
		return Root{}, errors.Wrap(err, "encode root")
	}
	data, _, err := c.do(ctx, http.MethodPut, "/sync/v3/root", http.Header{"Content-Type": {"application/json"}}, body)
	if err != nil {
		return Root{}, err
	}
	var root Root
	if err := json.Unmarshal(data, &root); err != nil {
		return Root{}, errors.Wrap(err, "decode root update")
	}
	return root, nil
}

func (c *Client) GetBlob(ctx context.Context, hash, filename string) ([]byte, error) {
	data, _, err := c.do(ctx, http.MethodGet, "/sync/v3/files/"+hash, http.Header{"rm-filename": {filename}}, nil)
	return data, err
}

func (c *Client) PutBlob(ctx context.Context, filename string, data []byte) (string, error) {
	hash := HashBytes(data)
	crc := crc32.Checksum(data, crc32cTable)
	var crcBytes [4]byte
	binary.BigEndian.PutUint32(crcBytes[:], crc)
	headers := http.Header{
		"rm-filename": {filename},
		"x-goog-hash": {"crc32c=" + base64.StdEncoding.EncodeToString(crcBytes[:])},
	}
	if _, _, err := c.do(ctx, http.MethodPut, "/sync/v3/files/"+hash, headers, data); err != nil {
		return "", err
	}
	return hash, nil
}

func HashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
