package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
)

// WSSDialer opens production device sessions through coder/websocket.
type WSSDialer struct{}

// Dial implements Dialer.
func (WSSDialer) Dial(ctx context.Context, endpoint string, headers http.Header, tlsConfig *tls.Config) (Connection, error) {
	conn, response, err := websocket.Dial(ctx, endpoint, &websocket.DialOptions{
		HTTPHeader: headers,
		HTTPClient: &http.Client{Transport: &http.Transport{TLSClientConfig: tlsConfig}},
	})
	if response != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return nil, fmt.Errorf("dial websocket: %w", err)
	}
	conn.SetReadLimit(maxFrameBytes)
	return conn, nil
}
