package market

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

type httpProvider struct {
	name     string
	client   *resty.Client
	limiter  *rateLimiter
	archiver RawArchiver
}

func newHTTPProvider(name string, timeout time.Duration, limiter *rateLimiter, archiver RawArchiver) *httpProvider {
	client := resty.New().
		SetTimeout(timeout).
		SetRetryCount(2)
	return &httpProvider{
		name:     name,
		client:   client,
		limiter:  limiter,
		archiver: archiver,
	}
}

func (h *httpProvider) Name() string {
	return h.name
}

func (h *httpProvider) get(ctx context.Context, url string, query map[string]string, kind string) ([]byte, string, error) {
	if err := h.limiter.Wait(ctx); err != nil {
		return nil, "", err
	}
	response, err := h.client.R().
		SetContext(ctx).
		SetQueryParams(query).
		Get(url)
	if err != nil {
		return nil, "", err
	}
	if response.StatusCode() >= 400 {
		return nil, "", fmt.Errorf("%s status %d", h.name, response.StatusCode())
	}
	var objectKey string
	if h.archiver != nil {
		objectKey, _ = h.archiver.Archive(ctx, h.name, kind, response.Body())
	}
	return response.Body(), objectKey, nil
}
