// Package client implements the device half of the authenticated gateway WSS
// session. It is shared by dotsim and echod; composition roots provide their
// hardware-specific turn sources and configuration consumers.
package client

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

const (
	maxFrameBytes = 64 << 10
	maxLogRecords = 256
	handshakeWait = 5 * time.Second
)

var (
	// ErrInsecureURL prevents a device from accidentally sending its bearer
	// credential over a plaintext WebSocket.
	ErrInsecureURL = errors.New("device client: gateway URL must use wss")
	ErrNoSession   = errors.New("device client: gateway is not connected")
	ErrTurnActive  = errors.New("device client: a turn is already active")
)

// Connection is the small transport surface the client needs. It makes all
// session behavior deterministic in tests without a real socket.
type Connection interface {
	Read(context.Context) (websocket.MessageType, []byte, error)
	Write(context.Context, websocket.MessageType, []byte) error
	Ping(context.Context) error
	Close(websocket.StatusCode, string) error
}

// Dialer opens a WSS connection. Implementations must honor tlsConfig.
type Dialer interface {
	Dial(context.Context, string, http.Header, *tls.Config) (Connection, error)
}

// Resolver implements the explicit, paired, then mDNS endpoint selection.
type Resolver interface {
	Resolve(context.Context, discovery.Config, *discovery.Instance) (string, error)
}

// PairingStore persists a gateway only after its authenticated welcome.
type PairingStore interface {
	Load() (*discovery.Instance, error)
	Save(discovery.Instance) error
}

// ConfigConsumer applies a validated revision atomically and returns the
// acknowledgement that must be sent to the gateway.
type ConfigConsumer interface {
	Apply(protocol.DeviceConfig) protocol.ConfigResult
}

// TurnSource waits for a locally-triggered, already endpointed turn. It never
// exposes idle microphone audio: calling Next may block while the device is
// locally listening for a wake word or button press.
type TurnSource interface {
	Next(context.Context) (Turn, error)
}

// Clock supplies time and sleeps so reconnect tests need no wall-clock waits.
type Clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

// Jitter chooses the delay in [0, limit). It is deliberately injected because
// reconnect timing is a behavioral contract, not an implementation detail.
type Jitter interface {
	Duration(time.Duration) time.Duration
}

// Options configures a shared session client.
type Options struct {
	Discovery     discovery.Config
	Hello         protocol.Hello
	Dialer        Dialer
	Resolver      Resolver
	Pairings      PairingStore
	Config        ConfigConsumer
	TurnSource    TurnSource
	TokenPath     string
	Clock         Clock
	Jitter        Jitter
	SkipTLSVerify bool
	Logger        *slog.Logger
	// TurnSent runs after the terminal audio.stop frame for a local turn has
	// reached the connection. It is intended for composition roots that need
	// to stop after a bounded fixture, without racing queued PCM writes.
	TurnSent func()
}

// Client maintains one reconnecting device session.
type Client struct {
	opts Options

	mu     sync.Mutex
	conn   Connection
	active bool
	high   chan outbound
	logs   chan protocol.LogRecord
}

type outbound struct {
	type_ websocket.MessageType
	data  []byte
	done  func()
}

// New validates options and creates a disconnected client.
func New(opts Options) (*Client, error) {
	if opts.Dialer == nil || opts.Resolver == nil || opts.Config == nil {
		return nil, errors.New("device client: dialer, resolver, and config consumer are required")
	}
	if opts.Clock == nil {
		opts.Clock = systemClock{}
	}
	if opts.Jitter == nil {
		opts.Jitter = randomJitter{}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Hello.Protocol == 0 {
		opts.Hello.Protocol = protocol.ProtocolVersion
	}
	if opts.Hello.Protocol != protocol.ProtocolVersion {
		return nil, errors.New("device client: unsupported hello protocol")
	}
	return &Client{opts: opts, high: make(chan outbound, 64), logs: make(chan protocol.LogRecord, maxLogRecords)}, nil
}

// Run reconnects until ctx is canceled. A failed connection is never trusted
// as a pairing candidate; pairing is saved only after a valid welcome.
func (c *Client) Run(ctx context.Context) error {
	backoff := 500 * time.Millisecond
	usePairing := true
	for {
		if ctx.Err() != nil {
			return fmt.Errorf("gateway session canceled: %w", context.Cause(ctx))
		}
		welcomed, err := c.runOnce(ctx, usePairing)
		if ctx.Err() != nil {
			return fmt.Errorf("gateway session canceled: %w", context.Cause(ctx))
		}
		if welcomed {
			backoff = 500 * time.Millisecond
			usePairing = true
		} else {
			// A stale paired address must not prevent the next attempt from
			// browsing mDNS for the gateway by its stable server identity.
			usePairing = false
		}
		delay := c.opts.Jitter.Duration(backoff)
		c.opts.Logger.Warn("gateway session ended", "error", err, "reconnect_delay", delay)
		if err := c.opts.Clock.Sleep(ctx, delay); err != nil {
			return fmt.Errorf("wait to reconnect: %w", err)
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (c *Client) runOnce(ctx context.Context, usePairing bool) (bool, error) {
	var paired *discovery.Instance
	if usePairing {
		paired = c.loadPairing()
	}
	endpoint, err := c.opts.Resolver.Resolve(ctx, c.opts.Discovery, paired)
	if err != nil {
		return false, fmt.Errorf("resolve gateway: %w", err)
	}
	if wssErr := requireWSS(endpoint); wssErr != nil {
		return false, wssErr
	}
	token, err := LoadToken(c.opts.TokenPath)
	if err != nil {
		return false, err
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if c.opts.SkipTLSVerify {
		tlsConfig.InsecureSkipVerify = true
		c.opts.Logger.Warn("TLS certificate verification disabled", "security_mode", "development", "tls_skip_verify", true)
	}
	conn, err := c.opts.Dialer.Dial(ctx, endpoint, headers, tlsConfig)
	if err != nil {
		return false, fmt.Errorf("dial gateway: %w", err)
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "session ended") }()
	if handshakeErr := c.handshake(ctx, conn, endpoint); handshakeErr != nil {
		return false, handshakeErr
	}
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	sessionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer c.disconnect(conn)

	errCh := make(chan error, 2)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); errCh <- c.writer(sessionCtx, conn) }()
	go func() { defer workers.Done(); errCh <- c.reader(sessionCtx, conn) }()
	if c.opts.TurnSource != nil {
		workers.Go(func() { c.forwardTurns(sessionCtx) })
	}
	err = <-errCh
	// Both reader and writer share the connection and queues. Stop and wait for
	// the surviving worker before a reconnect can create a replacement session.
	cancel()
	_ = conn.Close(websocket.StatusGoingAway, "session ended")
	workers.Wait()
	c.drainHigh()
	return true, err
}

func (c *Client) forwardTurns(ctx context.Context) {
	for {
		turn, err := c.opts.TurnSource.Next(ctx)
		if err != nil {
			if !errors.Is(err, io.EOF) && ctx.Err() == nil {
				c.opts.Logger.Warn("turn source ended", "error", err)
			}
			return
		}
		if err := c.SendTurn(ctx, turn); err != nil && ctx.Err() == nil {
			c.opts.Logger.Warn("discard turn after session failure", "error", err)
		}
	}
}

func (c *Client) handshake(ctx context.Context, conn Connection, endpoint string) error {
	if err := c.writeControl(ctx, conn, protocol.TypeHello, "", c.opts.Hello); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}
	handshakeCtx, cancel := context.WithTimeout(ctx, handshakeWait)
	defer cancel()
	type_, payload, err := conn.Read(handshakeCtx)
	if err != nil {
		return fmt.Errorf("read welcome: %w", err)
	}
	if type_ != websocket.MessageText {
		return errors.New("device client: welcome must be a text frame")
	}
	env, err := protocol.Decode(payload)
	if err != nil {
		return fmt.Errorf("decode welcome: %w", err)
	}
	if env.Type != protocol.TypeWelcome {
		return fmt.Errorf("device client: first gateway frame must be welcome, got %q", env.Type)
	}
	var welcome protocol.Welcome
	if err = env.DecodePayload(&welcome); err != nil {
		return fmt.Errorf("decode welcome payload: %w", err)
	}
	result := c.opts.Config.Apply(welcome.Config)
	if err = result.Validate(); err != nil {
		return fmt.Errorf("config consumer returned invalid result: %w", err)
	}
	if err = c.writeControl(ctx, conn, protocol.TypeConfigResult, "", result); err != nil {
		return fmt.Errorf("send config result: %w", err)
	}
	if c.opts.Pairings != nil {
		inst, instanceErr := pairingInstance(endpoint, welcome.ServerID)
		if instanceErr != nil {
			return instanceErr
		}
		if err = c.opts.Pairings.Save(inst); err != nil {
			return fmt.Errorf("save authenticated gateway pairing: %w", err)
		}
	}
	return nil
}

func (c *Client) writer(ctx context.Context, conn Connection) error {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case item := <-c.high:
			if err := c.writeOutbound(ctx, conn, item); err != nil {
				return fmt.Errorf("write high-priority frame: %w", err)
			}
			continue
		default:
		}
		select {
		case <-ctx.Done():
			return nil
		case item := <-c.high:
			if err := c.writeOutbound(ctx, conn, item); err != nil {
				return fmt.Errorf("write high-priority frame: %w", err)
			}
		case record := <-c.logs:
			// A high-priority producer may have won the blocking select just
			// after the optimistic poll above. Recheck before committing a log.
			select {
			case item := <-c.high:
				if err := c.writeOutbound(ctx, conn, item); err != nil {
					return fmt.Errorf("write high-priority frame: %w", err)
				}
				select {
				case c.logs <- record:
				default:
				}
				continue
			default:
			}
			data, err := protocol.Encode(protocol.TypeLog, "", c.opts.Clock.Now(), sanitizeRecord(record))
			if err != nil {
				return fmt.Errorf("encode log: %w", err)
			}
			if len(data) > maxFrameBytes {
				return errors.New("device client: log frame exceeds 64 KiB")
			}
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				return fmt.Errorf("write log: %w", err)
			}
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				return fmt.Errorf("heartbeat: %w", err)
			}
		}
	}
}

func (c *Client) writeOutbound(ctx context.Context, conn Connection, item outbound) error {
	if err := conn.Write(ctx, item.type_, item.data); err != nil {
		return fmt.Errorf("write outbound frame: %w", err)
	}
	if item.done != nil {
		item.done()
	}
	return nil
}

func (c *Client) reader(ctx context.Context, conn Connection) error {
	for {
		type_, payload, err := conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("read gateway frame: %w", err)
		}
		if len(payload) > maxFrameBytes {
			return errors.New("device client: inbound frame exceeds 64 KiB")
		}
		if type_ != websocket.MessageText {
			return errors.New("device client: unexpected binary gateway frame")
		}
		env, err := protocol.Decode(payload)
		if err != nil {
			return fmt.Errorf("decode gateway frame: %w", err)
		}
		switch env.Type {
		case protocol.TypeConfig:
			var value protocol.DeviceConfig
			if err := env.DecodePayload(&value); err != nil {
				return fmt.Errorf("decode config: %w", err)
			}
			result := c.opts.Config.Apply(value)
			if err := result.Validate(); err != nil {
				return fmt.Errorf("validate config result: %w", err)
			}
			if err := c.enqueueControl(protocol.TypeConfigResult, "", result); err != nil {
				return fmt.Errorf("queue config result: %w", err)
			}
		case protocol.TypePing:
			if err := c.enqueueControl(protocol.TypePong, "", nil); err != nil {
				return fmt.Errorf("queue pong: %w", err)
			}
		case protocol.TypePlayStart, protocol.TypePlayStop:
			return errors.New("device client: gateway playback is not implemented")
		default:
			// Unknown and currently unused protocol frames are forward-compatible.
		}
	}
}

// Log queues a sanitized low-priority record. It never blocks an audio turn;
// callers can use the false return to increment a local dropped-log metric.
func (c *Client) Log(record protocol.LogRecord) bool {
	record = sanitizeRecord(record)
	if record.Validate() != nil {
		return false
	}
	select {
	case c.logs <- record:
		return true
	default:
		return false
	}
}

// SendTurn sends one complete canonical PCM turn. It rejects disconnected and
// overlapping turns, and ensures all control framing precedes/follows PCM.
func (c *Client) SendTurn(ctx context.Context, turn Turn) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("send turn canceled: %w", context.Cause(ctx))
	}
	if err := turn.Validate(); err != nil {
		return err
	}
	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return ErrNoSession
	}
	if c.active {
		c.mu.Unlock()
		return ErrTurnActive
	}
	frameCount := 3
	for _, frame := range turn.PCM {
		if len(frame) > 0 {
			frameCount++
		}
	}
	if frameCount > cap(c.high)-len(c.high) {
		c.mu.Unlock()
		return errors.New("device client: turn exceeds available high-priority queue capacity")
	}
	c.active = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.active = false; c.mu.Unlock() }()
	if err := c.enqueueControl(protocol.TypeTurnStart, turn.ID, turn.Start); err != nil {
		return err
	}
	if err := c.enqueueControl(protocol.TypeAudioStart, turn.ID, protocol.AudioStart{SampleRate: 16000, Channels: 1, Format: protocol.AudioFormatPCMS16LE}); err != nil {
		return err
	}
	for _, frame := range turn.PCM {
		if len(frame) == 0 {
			continue
		}
		if len(frame) > maxFrameBytes || len(frame)%2 != 0 {
			return errors.New("device client: PCM frame must be even and at most 64 KiB")
		}
		if err := c.enqueueBinary(frame); err != nil {
			return err
		}
	}
	data, err := protocol.Encode(protocol.TypeAudioStop, turn.ID, c.opts.Clock.Now(), protocol.AudioStop{Reason: turn.Reason})
	if err != nil {
		return fmt.Errorf("encode control frame: %w", err)
	}
	if len(data) > maxFrameBytes {
		return errors.New("device client: control frame exceeds 64 KiB")
	}
	return c.enqueue(outbound{type_: websocket.MessageText, data: data, done: c.opts.TurnSent})
}

func (c *Client) enqueueControl(type_ protocol.MessageType, id string, payload any) error {
	data, err := protocol.Encode(type_, id, c.opts.Clock.Now(), payload)
	if err != nil {
		return fmt.Errorf("encode control frame: %w", err)
	}
	if len(data) > maxFrameBytes {
		return errors.New("device client: control frame exceeds 64 KiB")
	}
	return c.enqueue(outbound{type_: websocket.MessageText, data: data})
}
func (c *Client) enqueueBinary(data []byte) error {
	return c.enqueue(outbound{type_: websocket.MessageBinary, data: append([]byte(nil), data...)})
}
func (c *Client) enqueue(item outbound) error {
	c.mu.Lock()
	connected := c.conn != nil
	c.mu.Unlock()
	if !connected {
		return ErrNoSession
	}
	select {
	case c.high <- item:
		return nil
	default:
		return errors.New("device client: high-priority queue full")
	}
}
func (c *Client) writeControl(ctx context.Context, conn Connection, type_ protocol.MessageType, id string, payload any) error {
	data, err := protocol.Encode(type_, id, c.opts.Clock.Now(), payload)
	if err != nil {
		return fmt.Errorf("encode control frame: %w", err)
	}
	if len(data) > maxFrameBytes {
		return errors.New("device client: control frame exceeds 64 KiB")
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("write control frame: %w", err)
	}
	return nil
}
func (c *Client) disconnect(conn Connection) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
	}
	c.mu.Unlock()
}
func (c *Client) drainHigh() {
	for {
		select {
		case <-c.high:
		default:
			return
		}
	}
}
func (c *Client) loadPairing() *discovery.Instance {
	if c.opts.Pairings == nil {
		return nil
	}
	inst, err := c.opts.Pairings.Load()
	if err != nil && !errors.Is(err, discovery.ErrNoPairing) {
		c.opts.Logger.Warn("ignore unusable paired gateway", "error", err)
	}
	return inst
}

// Turn is one device-originated active audio window. PCM has already been
// canonicalized to mono 16 kHz signed little-endian samples by the device.
type Turn struct {
	ID     string
	Start  protocol.TurnStart
	PCM    [][]byte
	Reason protocol.AudioStopReason
}

func (t Turn) Validate() error {
	if strings.TrimSpace(t.ID) == "" || !t.Start.Trigger.Valid() || !t.Reason.Valid() {
		return errors.New("device client: invalid turn")
	}
	return nil
}

// LoadToken reads a development bearer token without retaining whitespace.
func LoadToken(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("device client: token file is required")
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: token path is supplied by the device composition root.
	if err != nil {
		return "", fmt.Errorf("read gateway token: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if len(token) < 32 {
		return "", errors.New("device client: token must contain at least 32 bytes")
	}
	return token, nil
}
func requireWSS(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || !strings.EqualFold(u.Scheme, "wss") {
		return ErrInsecureURL
	}
	return nil
}
func pairingInstance(raw, serverID string) (discovery.Instance, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return discovery.Instance{}, fmt.Errorf("parse paired endpoint: %w", err)
	}
	port := u.Port()
	p := discovery.DefaultPort
	if port != "" {
		_, err = fmt.Sscanf(port, "%d", &p)
		if err != nil {
			return discovery.Instance{}, fmt.Errorf("parse paired endpoint port: %w", err)
		}
	}
	return discovery.Instance{ServerID: serverID, Host: u.Hostname(), Port: p, TXT: discovery.TXTRecord{Protocol: protocol.ProtocolVersion, ServerID: serverID, TLS: true, Path: u.Path}}, nil
}
func sanitizeRecord(record protocol.LogRecord) protocol.LogRecord {
	const limit = 256
	const maxFields = 64
	fields := make(map[string]string, min(len(record.Fields), maxFields))
	for key, value := range record.Fields {
		lowered := strings.ToLower(key)
		if len(fields) == maxFields {
			break
		}
		if len(key) > limit {
			key = key[:limit]
		}
		if strings.Contains(lowered, "token") || strings.Contains(lowered, "secret") || strings.Contains(lowered, "authorization") || strings.Contains(lowered, "password") || strings.Contains(lowered, "credential") || strings.Contains(lowered, "api_key") || strings.Contains(lowered, "bearer") || strings.Contains(lowered, "cookie") || strings.Contains(lowered, "session") {
			fields[key] = "[redacted]"
			continue
		}
		if len(value) > limit {
			value = value[:limit]
		}
		fields[key] = value
	}
	record.Fields = fields
	if len(record.Message) > limit {
		record.Message = record.Message[:limit]
	}
	return record
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait canceled: %w", context.Cause(ctx))
	case <-timer.C:
		return nil
	}
}

type randomJitter struct{}

func (randomJitter) Duration(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0
	}
	return time.Duration(value.Int64())
}

// ConstantTimeEqualToken is available to server-side callers and tests that
// need to compare tokens without leaking a prefix length.
func ConstantTimeEqualToken(expected, actual string) bool {
	return subtle.ConstantTimeCompare([]byte(expected), []byte(actual)) == 1
}
