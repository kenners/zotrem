package zotero

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cockroachdb/errors"
)

type FileInfo struct {
	MD5   string
	Mtime int64
	Size  int64
}

func MD5Hex(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

func (c *Client) DownloadFile(ctx context.Context, key string) ([]byte, FileInfo, error) {
	path, err := c.libraryPath("/items/" + key + "/file")
	if err != nil {
		return nil, FileInfo{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, FileInfo{}, errors.Wrap(err, "build request")
	}
	req.Header.Set("Zotero-API-Version", "3")
	req.Header.Set("Zotero-API-Key", c.apiKey)
	noRedirect := *c.http
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noRedirect.Do(req)
	if err != nil {
		return nil, FileInfo{}, errors.Wrapf(err, "GET %s", path)
	}
	defer resp.Body.Close()
	info := FileInfo{MD5: resp.Header.Get("Zotero-File-MD5")}
	info.Mtime, _ = strconv.ParseInt(resp.Header.Get("Zotero-File-Modification-Time"), 10, 64)
	info.Size, _ = strconv.ParseInt(resp.Header.Get("Zotero-File-Size"), 10, 64)
	switch {
	case resp.StatusCode == http.StatusOK:
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, FileInfo{}, errors.Wrap(err, "read file")
		}
		return data, info, nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		location := resp.Header.Get("Location")
		if location == "" {
			return nil, FileInfo{}, errors.New("zotero file redirect without location")
		}
		target, err := url.Parse(location)
		if err != nil {
			return nil, FileInfo{}, errors.Wrap(err, "parse redirect")
		}
		data, err := c.fetchExternal(ctx, req.URL.ResolveReference(target).String())
		if err != nil {
			return nil, FileInfo{}, err
		}
		if info.MD5 == "" {
			info.MD5 = MD5Hex(data)
		}
		if info.Size == 0 {
			info.Size = int64(len(data))
		}
		return data, info, nil
	default:
		body, _ := io.ReadAll(resp.Body)
		return nil, FileInfo{}, &APIError{Body: string(body), Method: http.MethodGet, Path: path, Status: resp.StatusCode}
	}
}

func (c *Client) fetchExternal(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.Wrap(err, "build download request")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.Wrap(err, "download file")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, "read file")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Newf("download file: %d %s", resp.StatusCode, bytes.TrimSpace(data))
	}
	return data, nil
}

type uploadAuthorization struct {
	ContentType string `json:"contentType"`
	Exists      int    `json:"exists"`
	Prefix      string `json:"prefix"`
	Suffix      string `json:"suffix"`
	UploadKey   string `json:"uploadKey"`
	URL         string `json:"url"`
}

type UploadRequest struct {
	ContentType string
	Data        []byte
	ExistingMD5 string
	Filename    string
	Key         string
	Mtime       time.Time
}

func (c *Client) UploadFile(ctx context.Context, req UploadRequest) (int, error) {
	path, err := c.libraryPath("/items/" + req.Key + "/file")
	if err != nil {
		return 0, err
	}
	mtime := req.Mtime
	if mtime.IsZero() {
		mtime = time.Now()
	}
	sum := MD5Hex(req.Data)
	precondition := http.Header{"Content-Type": {"application/x-www-form-urlencoded"}}
	if req.ExistingMD5 == "" {
		precondition.Set("If-None-Match", "*")
	} else {
		precondition.Set("If-Match", req.ExistingMD5)
	}
	form := url.Values{
		"filename": {req.Filename},
		"filesize": {strconv.Itoa(len(req.Data))},
		"md5":      {sum},
		"mtime":    {strconv.FormatInt(mtime.UnixMilli(), 10)},
	}
	if req.ContentType != "" {
		form.Set("contentType", req.ContentType)
	}
	resp, err := c.do(ctx, http.MethodPost, path, nil, precondition, []byte(form.Encode()))
	if err != nil {
		return 0, errors.Wrap(err, "authorize upload")
	}
	var auth uploadAuthorization
	if err := json.Unmarshal(resp.Body, &auth); err != nil {
		return 0, errors.Wrap(err, "decode upload authorization")
	}
	if auth.Exists == 1 {
		return resp.Version, nil
	}
	if auth.URL == "" || auth.UploadKey == "" {
		return 0, errors.New("upload authorization missing url or upload key")
	}
	body := make([]byte, 0, len(auth.Prefix)+len(req.Data)+len(auth.Suffix))
	body = append(body, auth.Prefix...)
	body = append(body, req.Data...)
	body = append(body, auth.Suffix...)
	storeReq, err := http.NewRequestWithContext(ctx, http.MethodPost, auth.URL, bytes.NewReader(body))
	if err != nil {
		return 0, errors.Wrap(err, "build storage request")
	}
	storeReq.Header.Set("Content-Type", auth.ContentType)
	storeResp, err := c.http.Do(storeReq)
	if err != nil {
		return 0, errors.Wrap(err, "upload to storage")
	}
	storeBody, _ := io.ReadAll(storeResp.Body)
	storeResp.Body.Close()
	if storeResp.StatusCode < 200 || storeResp.StatusCode >= 300 {
		return 0, errors.Newf("storage upload failed: %d %s", storeResp.StatusCode, bytes.TrimSpace(storeBody))
	}
	register := url.Values{"upload": {auth.UploadKey}}
	resp, err = c.do(ctx, http.MethodPost, path, nil, precondition, []byte(register.Encode()))
	if err != nil {
		return 0, errors.Wrap(err, "register upload")
	}
	return resp.Version, nil
}
