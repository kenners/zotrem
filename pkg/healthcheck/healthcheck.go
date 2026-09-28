package healthcheck

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	Fail    = "fail"
	Log     = "log"
	Start   = "start"
	Success = ""
)

const MaxBody = 10000

type Client struct {
	http *http.Client
}

func New(client *http.Client) *Client {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{http: client}
}

func Normalize(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errors.Newf("%q is not an http or https URL", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String(), nil
}

func SignalURL(base, signal string) (string, error) {
	u, err := url.Parse(base)
	if err != nil {
		return "", errors.Wrap(err, "parse healthcheck url")
	}
	if signal != Success {
		u.Path = strings.TrimRight(u.Path, "/") + "/" + signal
	}
	return u.String(), nil
}

func (c *Client) Ping(ctx context.Context, base, signal, body string) error {
	target, err := SignalURL(base, signal)
	if err != nil {
		return err
	}
	if len(body) > MaxBody {
		body = strings.ToValidUTF8(body[:MaxBody], "")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
	if err != nil {
		return errors.Wrap(err, "build healthcheck request")
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.Wrap(err, "send healthcheck ping")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return errors.Newf("healthcheck ping answered %s", resp.Status)
	}
	return nil
}
