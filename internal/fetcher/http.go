package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	maxRetries     = 3
	requestTimeout = 5 * time.Second
	initialBackoff = 200 * time.Millisecond
)

type HTTPFetcher struct {
	baseURL    string
	httpClient *http.Client
	procHandle *processHandle
}

func NewHTTPFetcher(baseURL string, pid int32) *HTTPFetcher {
	ph := newProcessHandle(pid)
	return &HTTPFetcher{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        2,
				MaxIdleConnsPerHost: 2,
				IdleConnTimeout:     30 * time.Second,
			},
		},
		procHandle: ph,
	}
}

func (f *HTTPFetcher) Fetch(ctx context.Context) (*Snapshot, error) {
	var (
		threads ThreadsResponse
		metrics MetricsSnapshot
		proc    ProcessMetrics
		mu      sync.Mutex
		errs    []string
	)

	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		t, err := f.fetchThreads(ctx)
		if err != nil {
			mu.Lock()
			errs = append(errs, err.Error())
			mu.Unlock()
			return nil
		}
		threads = t
		return nil
	})

	g.Go(func() error {
		m, err := f.fetchMetrics(ctx)
		if err != nil {
			mu.Lock()
			errs = append(errs, err.Error())
			mu.Unlock()
			return nil
		}
		metrics = m
		return nil
	})

	g.Go(func() error {
		p, err := f.procHandle.fetch(ctx)
		if err != nil {
			mu.Lock()
			errs = append(errs, err.Error())
			mu.Unlock()
			return nil
		}
		proc = p
		return nil
	})

	_ = g.Wait()

	return &Snapshot{
		Threads:   threads,
		Metrics:   metrics,
		Process:   proc,
		FetchedAt: time.Now(),
		Errors:    errs,
	}, nil
}

func (f *HTTPFetcher) RestartWorkers(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.baseURL+"/frankenphp/workers/restart", nil)
	if err != nil {
		return err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("restart workers: %w", err)
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("restart workers: HTTP %d", resp.StatusCode)
	}
	return nil
}

func (f *HTTPFetcher) fetchThreads(ctx context.Context) (ThreadsResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/frankenphp/threads", nil)
	if err != nil {
		return ThreadsResponse{}, err
	}

	resp, err := f.doWithRetry(ctx, req)
	if err != nil {
		return ThreadsResponse{}, fmt.Errorf("fetch threads: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ThreadsResponse{}, fmt.Errorf("fetch threads: HTTP %d", resp.StatusCode)
	}

	var result ThreadsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ThreadsResponse{}, fmt.Errorf("fetch threads: decode: %w", err)
	}
	return result, nil
}

func (f *HTTPFetcher) fetchMetrics(ctx context.Context) (MetricsSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/metrics", nil)
	if err != nil {
		return MetricsSnapshot{}, err
	}

	resp, err := f.doWithRetry(ctx, req)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("fetch metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return MetricsSnapshot{}, fmt.Errorf("fetch metrics: HTTP %d", resp.StatusCode)
	}

	return parsePrometheusMetrics(resp.Body)
}

func (f *HTTPFetcher) doWithRetry(ctx context.Context, req *http.Request) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := initialBackoff * time.Duration(1<<uint(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
			req = req.Clone(ctx)
		}
		resp, err := f.httpClient.Do(req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
