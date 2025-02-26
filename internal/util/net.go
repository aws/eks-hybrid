package util

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"time"

	"github.com/pkg/errors"

	"github.com/aws/eks-hybrid/cmd/nodeadm/version"
)

const userAgentHeader = "User-Agent"

var userAgent = fmt.Sprintf("nodeadm/%s (%s/%s)", version.GitVersion, runtime.GOOS, runtime.GOARCH)

func GetHttpFile(ctx context.Context, uri string) ([]byte, error) {
	reader, err := GetHttpFileReader(ctx, uri)
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, errors.Wrapf(err, "failed reading file from url: %s", uri)
	}

	return data, nil
}

func GetHttpFileReader(ctx context.Context, uri string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, errors.Wrapf(err, "failed creating request from url: %s", uri)
	}
	request.Header.Add(userAgentHeader, userAgent)

	httpRetryClient := newRetryableHttpClient(2*time.Second, 3)
	resp, err := httpRetryClient.Do(request)
	if err != nil {
		return nil, errors.Wrapf(err, "failed reading file from url: %s", uri)
	}
	return resp.Body, nil
}

type retryHttpClient struct {
	backoff    time.Duration
	maxRetries int
}

func newRetryableHttpClient(backoff time.Duration, maxRetries int) *retryHttpClient {
	return &retryHttpClient{
		backoff:    backoff,
		maxRetries: maxRetries,
	}
}

func (hc *retryHttpClient) Do(req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	for range hc.maxRetries {
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			err = fmt.Errorf("unexpected status code: %d", resp.StatusCode)
			continue
		}
		return resp, nil
	}
	return nil, fmt.Errorf("max retries achieved for http request: %s : %w", req.Host, err)
}
