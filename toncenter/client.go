package toncenter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type Client struct {
	http    *http.Client
	baseURL string // e.g. "https://toncenter.com"
	apiKey  string // used as X-API-Key; if empty, no auth header is set

	rl *slidingLimiter
}

// Option configures Client.
type Option func(*Client)

// WithAPIKey sets X-API-Key (and optional query api_key).
func WithAPIKey(key string) Option {
	return func(c *Client) {
		c.apiKey = key
	}
}

// WithHTTPClient allows custom http.Client (retries, tracing, proxy, etc).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// WithRateLimit limits requests per second, set 0 for no limit, default 0
func WithRateLimit(maxPerSec float64) Option {
	return func(c *Client) {
		if maxPerSec > 0 {
			period := 1 * time.Second
			if maxPerSec < 1 {
				period = time.Duration(math.Round(float64(time.Second) / maxPerSec))
				if period < time.Millisecond {
					period = time.Millisecond
				}
				maxPerSec = 1
			}

			c.rl = newSlidingLimiter(int(maxPerSec), period)
		}
	}
}

// WithTimeout sets http.Client timeout if a default client is used.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if c.http == nil {
			c.http = &http.Client{Timeout: d}
			return
		}
		c.http.Timeout = d
	}
}

func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
			},
		},
	}

	for _, opt := range opts {
		opt(c)
	}
	return c
}

type slidingLimiter struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	times  []time.Time // отсортировано по возрастанию; моменты стартов запросов
}

func newSlidingLimiter(max int, window time.Duration) *slidingLimiter {
	return &slidingLimiter{
		window: window,
		max:    max,
		times:  make([]time.Time, 0, max),
	}
}

func (l *slidingLimiter) wait(ctx context.Context) error {
	for {
		now := time.Now()
		cutoff := now.Add(-l.window)

		l.mu.Lock()
		i := 0
		for i < len(l.times) && l.times[i].Before(cutoff) {
			i++
		}
		if i > 0 {
			l.times = l.times[i:]
		}

		if len(l.times) < l.max {
			l.times = append(l.times, now)
			l.mu.Unlock()
			return nil
		}

		waitUntil := l.times[0].Add(l.window)
		l.mu.Unlock()

		d := time.Until(waitUntil)
		if d <= 0 {
			continue
		}

		timer := time.NewTimer(d)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

type Response[T any] struct {
	Ok     bool   `json:"ok"`
	Result T      `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	Code   *int   `json:"code,omitempty"`
}

func doGET[T any](ctx context.Context, c *Client, path string, q url.Values, v3 bool) (*T, error) {
	if q == nil {
		q = url.Values{}
	}

	u := path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}

	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey) // supported in spec
	}
	return do[T](c, req, v3)
}

func doPOST[T any](ctx context.Context, c *Client, path string, body any, v3 bool) (*T, error) {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return nil, err
		}
	}

	u := path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &buf)
	if err != nil {
		return nil, err
	}

	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")

	return do[T](c, req, v3)
}

func do[T any](c *Client, req *http.Request, isV3 bool) (*T, error) {
	if c.rl != nil {
		if err := c.rl.wait(req.Context()); err != nil {
			return nil, err
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 150<<20)) // 150MB cap
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		type errorTc struct {
			Error string `json:"error"`
		}

		var tr errorTc
		if err = json.Unmarshal(body, &tr); err != nil {
			return nil, fmt.Errorf("toncenter response decode error, status code %d: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("toncenter api error, status code %d: %s, %s", resp.StatusCode, tr.Error, string(body))
	}

	if isV3 {
		var tr T
		if err = json.Unmarshal(body, &tr); err != nil {
			return nil, fmt.Errorf("toncenter api respose parse error: %w", err)
		}

		return &tr, nil
	}

	var tr Response[T]
	if err := json.Unmarshal(body, &tr); err != nil {
		var trErr Response[string]
		if err := json.Unmarshal(body, &trErr); err == nil && !trErr.Ok && trErr.Code != nil {
			return nil, fmt.Errorf("toncenter api error, code %d: %s", *trErr.Code, trErr.Result)
		}

		return nil, fmt.Errorf("decode error: %w; body=%s", err, string(body))
	}

	// HTTP 504 is possible (Lite Server Timeout) per spec; API still returns JSON envelope.
	if !tr.Ok {
		return nil, fmt.Errorf("toncenter api error: %s", tr.Error)
	}
	return &tr.Result, nil
}
