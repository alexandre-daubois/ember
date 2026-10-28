package fetcher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type RemoteLogs struct {
	Epoch   int64      `json:"epoch"`
	Next    int64      `json:"next"`
	Entries []LogEntry `json:"entries"`
}

const MaxRemoteLogs = 1000

type RefusedError struct {
	error
	Status int
}

type RemoteFetcher struct {
	*HTTPFetcher
	base *url.URL
}

func NewRemoteFetcher(base *url.URL) *RemoteFetcher {
	return &RemoteFetcher{HTTPFetcher: NewHTTPFetcher(base.JoinPath("caddy").String(), 0), base: base}
}

func (f *RemoteFetcher) DialTLSCertificates(ctx context.Context, _ []string) []CertificateInfo {
	var certs []CertificateInfo
	if err := f.do(ctx, "/certificates", nil, &certs); err != nil {
		return nil
	}
	return certs
}

func (f *RemoteFetcher) RestartWorkers(context.Context) error {
	return errors.New("worker restart is not available in a remote session")
}

func (f *RemoteFetcher) FetchLogs(ctx context.Context, epoch, after int64) (*RemoteLogs, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	params := url.Values{"after": {strconv.FormatInt(after, 10)}}
	if after >= 0 {
		params.Set("epoch", strconv.FormatInt(epoch, 10))
	}
	var page RemoteLogs
	if err := f.do(ctx, "/logs", params, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func dropControl(r rune) rune {
	if unicode.IsControl(r) {
		return -1
	}
	return r
}

func (f *RemoteFetcher) do(ctx context.Context, path string, params url.Values, out any) error {
	u := f.base.JoinPath(path)
	u.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		// The answer may come from any server the URL reaches, and this error ends up on stderr.
		err := errors.New(strings.Map(dropControl, "daemon answered "+resp.Status+": "+strings.TrimSpace(string(body))))
		if resp.StatusCode < http.StatusInternalServerError {
			return RefusedError{err, resp.StatusCode}
		}
		return err
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode daemon %s: %w", strings.TrimPrefix(path, "/"), err)
	}
	return nil
}
