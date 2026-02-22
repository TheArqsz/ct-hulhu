package ctlog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	retries    int
}

func NewClient(baseURL string, timeout time.Duration, retries int) *Client {
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] != '/' {
		baseURL += "/"
	}
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 100,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		retries: retries,
	}
}

func (c *Client) GetSTH(ctx context.Context) (*STH, error) {
	url := c.baseURL + "ct/v1/get-sth"

	body, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("get-sth: %w", err)
	}

	var sth STH
	if err := json.Unmarshal(body, &sth); err != nil {
		return nil, fmt.Errorf("parsing STH: %w", err)
	}
	return &sth, nil
}

func (c *Client) GetRawEntries(ctx context.Context, start, end int64) (*GetEntriesResponse, error) {
	url := fmt.Sprintf("%sct/v1/get-entries?start=%d&end=%d", c.baseURL, start, end)

	body, err := c.doRequestWithRetry(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("get-entries [%d-%d]: %w", start, end, err)
	}

	var resp GetEntriesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parsing entries [%d-%d]: %w", start, end, err)
	}
	return &resp, nil
}

func (c *Client) doRequestWithRetry(ctx context.Context, url string) ([]byte, error) {
	var lastErr error

	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		body, err := c.doRequest(ctx, url)
		if err == nil {
			return body, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("all %d retries exhausted: %w", c.retries, lastErr)
}

const maxResponseSize = 64 << 20

func (c *Client) doRequest(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ct-hulhu")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	return io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
}
