package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// HTTPDownloader fetches update resources using the supplied paired-gateway
// client. The caller owns its TLS transport and authentication headers; this
// adapter neither replaces nor logs either. Redirects are rejected unless the
// target remains the same paired HTTPS authority.
type HTTPDownloader struct {
	Client           *http.Client
	GatewayAuthority string
	RequestHeaders   http.Header
}

func (d HTTPDownloader) Fetch(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	if err := validateGatewayURL(rawURL, d.GatewayAuthority); err != nil {
		return nil, err
	}
	if d.Client == nil {
		return nil, errors.New("update: HTTP client is required")
	}
	client := *d.Client
	previousRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := validateGatewayURL(req.URL.String(), d.GatewayAuthority); err != nil {
			return err
		}
		if previousRedirect != nil {
			return previousRedirect(req, via)
		}
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create update request: %w", err)
	}
	for key, values := range d.RequestHeaders {
		req.Header[key] = append([]string(nil), values...)
	}
	response, err := client.Do(req)
	if err != nil {
		// net/url.Error includes the request URL, whose query commonly carries a
		// deployment credential. Keep the externally returned error safe to log.
		return nil, ErrDownloadFailed
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_ = response.Body.Close()
		return nil, fmt.Errorf("fetch update resource: unexpected HTTP status %d", response.StatusCode)
	}
	return response.Body, nil
}
