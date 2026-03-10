package fetcher

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

type HTTPFetcher struct {
	baseURL    string
	httpClient *http.Client
	pid        int32
}

func NewHTTPFetcher(baseURL string, pid int32) *HTTPFetcher {
	return &HTTPFetcher{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 5 * time.Second},
		pid:        pid,
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
		p, err := fetchProcessMetrics(ctx, f.pid)
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

func (f *HTTPFetcher) fetchThreads(ctx context.Context) (ThreadsResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/frankenphp/threads", nil)
	if err != nil {
		return ThreadsResponse{}, err
	}

	resp, err := f.httpClient.Do(req)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.baseURL+"/metrics", nil)
	if err != nil {
		return MetricsSnapshot{}, err
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("fetch metrics: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return MetricsSnapshot{}, fmt.Errorf("fetch metrics: HTTP %d", resp.StatusCode)
	}

	return parsePrometheusMetrics(resp.Body)
}
