package decision

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type httpClient struct {
	key     string
	headers map[string]string
	timeout time.Duration
	// retryDelay is the base backoff; tests shorten it.
	retryDelay time.Duration
}

const maxAttempts = 4

func retryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return true
	}
	return false
}

// post sends body as JSON and decodes the JSON response into out, retrying
// rate limits, overloads, and transient server errors with backoff.
func (c *httpClient) post(ctx context.Context, url string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	delay := c.retryDelay
	if delay == 0 {
		delay = time.Second
	}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, data, retryAfter, err := c.once(ctx, url, payload)
		switch {
		case err != nil:
			lastErr = err
			if ctx.Err() != nil {
				return lastErr
			}
		case status >= 200 && status < 300:
			if err := json.Unmarshal(data, out); err != nil {
				return fmt.Errorf("invalid JSON response from %s: %w", url, err)
			}
			return nil
		default:
			lastErr = fmt.Errorf("%s returned HTTP %d: %s", url, status, snippet(data))
			if !retryable(status) {
				return lastErr
			}
		}
		if attempt == maxAttempts {
			break
		}
		wait := delay << (attempt - 1)
		if retryAfter > 0 {
			wait = min(retryAfter, 30*time.Second)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
	return lastErr
}

func (c *httpClient) once(ctx context.Context, url string, payload []byte) (int, []byte, time.Duration, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "aesthetics")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return 0, nil, 0, err
	}
	var retryAfter time.Duration
	if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && s > 0 {
		retryAfter = time.Duration(s) * time.Second
	}
	return resp.StatusCode, data, retryAfter, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	if s == "" {
		s = "(empty body)"
	}
	return s
}
