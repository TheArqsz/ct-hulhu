package loglist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultLogListURL = "https://www.gstatic.com/ct/log_list/v3/log_list.json"

type Fetcher struct {
	client *http.Client
}

func NewFetcher(timeout time.Duration) *Fetcher {
	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (f *Fetcher) Fetch(ctx context.Context, url string) (*LogList, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("User-Agent", "ct-hulhu")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching log list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	const maxLogListSize = 4 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxLogListSize))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}

	var logList LogList
	if err := json.Unmarshal(body, &logList); err != nil {
		return nil, fmt.Errorf("parsing log list JSON: %w", err)
	}

	return &logList, nil
}

func (f *Fetcher) FetchDefault(ctx context.Context) (*LogList, error) {
	return f.Fetch(ctx, DefaultLogListURL)
}

func FilterLogs(logList *LogList, stateFilter string) []LogWithOperator {
	var result []LogWithOperator
	for _, op := range logList.Operators {
		for _, log := range op.Logs {
			if log.MatchesState(stateFilter) {
				result = append(result, LogWithOperator{
					Log:      log,
					Operator: op.Name,
				})
			}
		}
	}
	return result
}

type LogWithOperator struct {
	Log      Log
	Operator string
}
