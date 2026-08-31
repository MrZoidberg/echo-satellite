package client

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/discovery"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

type testClock struct{}

func (testClock) Now() time.Time { return time.Unix(100, 0).UTC() }
func (testClock) Sleep(ctx context.Context, _ time.Duration) error {
	<-ctx.Done()
	return fmt.Errorf("test sleep canceled: %w", context.Cause(ctx))
}

type zeroJitter struct{}

func (zeroJitter) Duration(time.Duration) time.Duration { return 0 }

type fakeResolver struct {
	endpoint string
	err      error
}

func (r fakeResolver) Resolve(context.Context, discovery.Config, *discovery.Instance) (string, error) {
	return r.endpoint, r.err
}

type fakePairings struct {
	saved discovery.Instance
	load  *discovery.Instance
	err   error
}

func (p *fakePairings) Load() (*discovery.Instance, error) { return p.load, p.err }
func (p *fakePairings) Save(inst discovery.Instance) error { p.saved = inst; return nil }

type fakeConfig struct {
	result protocol.ConfigResult
	got    []protocol.DeviceConfig
}

func (c *fakeConfig) Apply(value protocol.DeviceConfig) protocol.ConfigResult {
	c.got = append(c.got, value)
	return c.result
}

type incoming struct {
	type_ websocket.MessageType
	data  []byte
	err   error
}
type fakeConn struct {
	reads  chan incoming
	mu     sync.Mutex
	writes []incoming
}

func newFakeConn() *fakeConn { return &fakeConn{reads: make(chan incoming, 8)} }
func (c *fakeConn) Read(ctx context.Context) (websocket.MessageType, []byte, error) {
	select {
	case event := <-c.reads:
		return event.type_, event.data, event.err
	case <-ctx.Done():
		return 0, nil, fmt.Errorf("test read canceled: %w", context.Cause(ctx))
	}
}
func (c *fakeConn) Write(_ context.Context, type_ websocket.MessageType, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, incoming{type_: type_, data: append([]byte(nil), data...)})
	return nil
}
func (c *fakeConn) Ping(context.Context) error               { return nil }
func (c *fakeConn) Close(websocket.StatusCode, string) error { return nil }

type fakeDialer struct {
	conn  Connection
	calls chan struct{}
}

func (d fakeDialer) Dial(context.Context, string, http.Header, *tls.Config) (Connection, error) {
	if d.calls != nil {
		d.calls <- struct{}{}
	}
	return d.conn, nil
}

type fakeTurnSource struct{ turns []Turn }

func (s *fakeTurnSource) Next(context.Context) (Turn, error) {
	if len(s.turns) == 0 {
		return Turn{}, io.EOF
	}
	turn := s.turns[0]
	s.turns = s.turns[1:]
	return turn, nil
}

func testDeviceConfig() protocol.DeviceConfig {
	return protocol.DeviceConfig{Version: 1, Wake: protocol.WakeSettings{Engine: "openwakeword", Model: "okay_nabu", Threshold: .5, VADEnabled: true, VADThreshold: .5, VADLookbackMS: 1200, PreRollMS: 600, MinIntervalMS: 2000, AlwaysScoreWake: true}, Endpointing: protocol.EndpointingConfig{SpeechThreshold: .5, SpeechOnsetMS: 160, TrailingSilenceMS: 1500, NoSpeechTimeoutMS: 3000, MaxTurnMS: 60000}, Logs: protocol.LogSettings{ForwardLevel: protocol.LogLevelInfo}}
}

func testClient(t *testing.T, config ConfigConsumer) *Client {
	t.Helper()
	client, err := New(Options{Hello: protocol.Hello{DeviceID: "dot", AgentVersion: "test", Protocol: protocol.ProtocolVersion}, Dialer: WSSDialer{}, Resolver: fakeResolver{endpoint: "wss://gateway.test/device"}, Config: config, Clock: testClock{}, Jitter: zeroJitter{}})
	require.NoError(t, err)
	return client
}

func TestHandshake_SendsHelloAppliesWelcomeAndPairs(t *testing.T) {
	consumer := &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}}
	client := testClient(t, consumer)
	pairings := &fakePairings{}
	client.opts.Pairings = pairings
	conn := newFakeConn()
	welcome, err := protocol.Encode(protocol.TypeWelcome, "", time.Now(), protocol.Welcome{ServerID: "home", Protocol: protocol.ProtocolVersion, Config: testDeviceConfig()})
	require.NoError(t, err)
	conn.reads <- incoming{type_: websocket.MessageText, data: welcome}

	require.NoError(t, client.handshake(t.Context(), conn, "wss://gateway.test:8770/device"))
	require.Len(t, consumer.got, 1)
	assert.Equal(t, "home", pairings.saved.ServerID)
	require.Len(t, conn.writes, 2)
	assert.Equal(t, websocket.MessageText, conn.writes[0].type_)
	hello, err := protocol.Decode(conn.writes[0].data)
	require.NoError(t, err)
	assert.Equal(t, protocol.TypeHello, hello.Type)
	result, err := protocol.Decode(conn.writes[1].data)
	require.NoError(t, err)
	assert.Equal(t, protocol.TypeConfigResult, result.Type)
}

func TestHandshake_RejectsNonWelcome(t *testing.T) {
	client := testClient(t, &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}})
	conn := newFakeConn()
	payload, err := protocol.Encode(protocol.TypePing, "", time.Now(), nil)
	require.NoError(t, err)
	conn.reads <- incoming{type_: websocket.MessageText, data: payload}
	assert.ErrorContains(t, client.handshake(t.Context(), conn, "wss://gateway.test/device"), "first gateway frame must be welcome")
}

func TestReader_ConfigAcknowledged(t *testing.T) {
	consumer := &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}}
	client := testClient(t, consumer)
	conn := newFakeConn()
	client.conn = conn
	payload, err := protocol.Encode(protocol.TypeConfig, "config-1", time.Now(), testDeviceConfig())
	require.NoError(t, err)
	conn.reads <- incoming{type_: websocket.MessageText, data: payload}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- client.reader(ctx, conn) }()
	require.Eventually(t, func() bool { return len(client.high) == 1 }, time.Second, time.Millisecond)
	cancel()
	<-errCh
	queued := <-client.high
	env, err := protocol.Decode(queued.data)
	require.NoError(t, err)
	assert.Equal(t, protocol.TypeConfigResult, env.Type)
	assert.Equal(t, []protocol.DeviceConfig{testDeviceConfig()}, consumer.got)
}

func TestWriter_PrioritizesTurnControlOverLogs(t *testing.T) {
	client := testClient(t, &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}})
	conn := newFakeConn()
	client.conn = conn
	require.True(t, client.Log(protocol.LogRecord{Level: protocol.LogLevelInfo, Message: "low"}))
	require.NoError(t, client.enqueueControl(protocol.TypePing, "", nil))
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() { errCh <- client.writer(ctx, conn) }()
	require.Eventually(t, func() bool { conn.mu.Lock(); defer conn.mu.Unlock(); return len(conn.writes) >= 2 }, time.Second, time.Millisecond)
	cancel()
	<-errCh
	conn.mu.Lock()
	defer conn.mu.Unlock()
	first, err := protocol.Decode(conn.writes[0].data)
	require.NoError(t, err)
	assert.Equal(t, protocol.TypePing, first.Type)
	second, err := protocol.Decode(conn.writes[1].data)
	require.NoError(t, err)
	assert.Equal(t, protocol.TypeLog, second.Type)
}

func TestSendTurn_UsesStrictControlBinaryControlOrder(t *testing.T) {
	client := testClient(t, &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}})
	client.conn = newFakeConn()
	require.NoError(t, client.SendTurn(t.Context(), Turn{ID: "turn-1", Start: protocol.TurnStart{Trigger: protocol.TriggerWake}, PCM: [][]byte{{1, 0}, {2, 0}}, Reason: protocol.AudioStopEndpointed}))
	require.Len(t, client.high, 5)
	types := make([]websocket.MessageType, 0, 5)
	var controls []protocol.MessageType
	for range 5 {
		item := <-client.high
		types = append(types, item.type_)
		if item.type_ == websocket.MessageText {
			env, err := protocol.Decode(item.data)
			require.NoError(t, err)
			controls = append(controls, env.Type)
		}
	}
	assert.Equal(t, []websocket.MessageType{websocket.MessageText, websocket.MessageText, websocket.MessageBinary, websocket.MessageBinary, websocket.MessageText}, types)
	assert.Equal(t, []protocol.MessageType{protocol.TypeTurnStart, protocol.TypeAudioStart, protocol.TypeAudioStop}, controls)
}

func TestWriterCallsTurnSentAfterTerminalAudioStop(t *testing.T) {
	sent := make(chan struct{}, 1)
	client := testClient(t, &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}})
	client.opts.TurnSent = func() { sent <- struct{}{} }
	conn := newFakeConn()
	client.conn = conn
	require.NoError(t, client.SendTurn(t.Context(), Turn{ID: "turn-1", Start: protocol.TurnStart{Trigger: protocol.TriggerButton}, Reason: protocol.AudioStopEOF}))
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() { errCh <- client.writer(ctx, conn) }()
	require.Eventually(t, func() bool { return len(sent) == 1 }, time.Second, time.Millisecond)
	cancel()
	<-errCh
}

func TestLog_SanitizesAndDropsUnderPressure(t *testing.T) {
	client := testClient(t, &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}})
	require.True(t, client.Log(protocol.LogRecord{Level: protocol.LogLevelInfo, Message: "ok", Fields: map[string]string{"token": "do-not-send", "api_key": "also-hidden", "cookie": "hidden", "safe": string(make([]byte, 300))}}))
	record := <-client.logs
	assert.Equal(t, "[redacted]", record.Fields["token"])
	assert.Equal(t, "[redacted]", record.Fields["api_key"])
	assert.Equal(t, "[redacted]", record.Fields["cookie"])
	assert.Len(t, record.Fields["safe"], 256)
	for range maxLogRecords {
		client.logs <- protocol.LogRecord{Level: protocol.LogLevelInfo, Message: "full"}
	}
	assert.False(t, client.Log(protocol.LogRecord{Level: protocol.LogLevelInfo, Message: "dropped"}))
}

func TestLoadTokenAndWSSRequirement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte("  01234567890123456789012345678901\n"), 0o600))
	token, err := LoadToken(path)
	require.NoError(t, err)
	assert.Len(t, token, 32)
	require.Error(t, requireWSS("ws://gateway.test/device"))
	require.NoError(t, requireWSS("wss://gateway.test/device"))
	assert.True(t, ConstantTimeEqualToken(token, token))
	assert.False(t, ConstantTimeEqualToken(token, "wrong"))
	_, err = LoadToken(filepath.Join(t.TempDir(), "missing"))
	require.Error(t, err)
	require.NoError(t, os.WriteFile(path, []byte("short"), 0o600))
	_, err = LoadToken(path)
	require.Error(t, err)
}

func TestTurnAndQueueRejectInvalidOrDisconnectedInput(t *testing.T) {
	client := testClient(t, &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}})
	require.ErrorIs(t, client.SendTurn(t.Context(), Turn{ID: "turn", Start: protocol.TurnStart{Trigger: protocol.TriggerWake}, Reason: protocol.AudioStopEOF}), ErrNoSession)
	client.conn = newFakeConn()
	require.Error(t, client.SendTurn(t.Context(), Turn{ID: "", Start: protocol.TurnStart{Trigger: protocol.TriggerWake}, Reason: protocol.AudioStopEOF}))
	require.Error(t, client.SendTurn(t.Context(), Turn{ID: "turn", Start: protocol.TurnStart{Trigger: protocol.TriggerWake}, PCM: [][]byte{{1}}, Reason: protocol.AudioStopEOF}))
	client.disconnect(client.conn)
	assert.ErrorIs(t, client.enqueueControl(protocol.TypePing, "", nil), ErrNoSession)
}

func TestTurnRejectsQueueExhaustionWithoutPartialFrames(t *testing.T) {
	client := testClient(t, &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}})
	client.conn = newFakeConn()
	for range cap(client.high) - 2 {
		client.high <- outbound{}
	}
	err := client.SendTurn(t.Context(), Turn{ID: "turn", Start: protocol.TurnStart{Trigger: protocol.TriggerWake}, Reason: protocol.AudioStopEOF})
	require.Error(t, err)
	assert.Len(t, client.high, cap(client.high)-2)
}

func TestWriteControlRejectsOversizedFrame(t *testing.T) {
	client := testClient(t, &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}})
	client.opts.Hello.AgentVersion = string(make([]byte, maxFrameBytes))
	require.Error(t, client.writeControl(t.Context(), newFakeConn(), protocol.TypeHello, "", client.opts.Hello))
}

func TestClockAndJitter(t *testing.T) {
	assert.False(t, systemClock{}.Now().IsZero())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, systemClock{}.Sleep(ctx, time.Hour), context.Canceled)
	assert.Zero(t, randomJitter{}.Duration(0))
	assert.Less(t, randomJitter{}.Duration(time.Second), time.Second)
}

func TestNew_RequiresDependencies(t *testing.T) { _, err := New(Options{}); assert.Error(t, err) }

func TestWSSDialerConnectsToTLSServer(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer func() { require.NoError(t, conn.Close(websocket.StatusNormalClosure, "done")) }()
	}))
	defer server.Close()
	endpoint := "wss" + strings.TrimPrefix(server.URL, "https")
	conn, err := (WSSDialer{}).Dial(t.Context(), endpoint, nil, &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}) //nolint:gosec // G402: test TLS server has a generated certificate.
	require.NoError(t, err)
	require.NoError(t, conn.Close(websocket.StatusNormalClosure, "done"))
}

func TestRun_AuthenticatesForwardsLocalTurnAndStopsOnCancel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(path, []byte("01234567890123456789012345678901"), 0o600))
	conn := newFakeConn()
	welcome, err := protocol.Encode(protocol.TypeWelcome, "", time.Now(), protocol.Welcome{ServerID: "home", Protocol: protocol.ProtocolVersion, Config: testDeviceConfig()})
	require.NoError(t, err)
	conn.reads <- incoming{type_: websocket.MessageText, data: welcome}
	dialed := make(chan struct{}, 1)
	client, err := New(Options{Hello: protocol.Hello{DeviceID: "dot", Protocol: protocol.ProtocolVersion}, TokenPath: path, Dialer: fakeDialer{conn: conn, calls: dialed}, Resolver: fakeResolver{endpoint: "wss://gateway.test/device"}, Config: &fakeConfig{result: protocol.ConfigResult{Version: 1, Status: protocol.ConfigResultApplied}}, Clock: testClock{}, Jitter: zeroJitter{}, TurnSource: &fakeTurnSource{turns: []Turn{{ID: "turn", Start: protocol.TurnStart{Trigger: protocol.TriggerButton}, PCM: [][]byte{{1, 0}}, Reason: protocol.AudioStopEOF}}}})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() { errCh <- client.Run(ctx) }()
	<-dialed
	require.Eventually(t, func() bool {
		conn.mu.Lock()
		defer conn.mu.Unlock()
		return len(conn.writes) >= 5
	}, time.Second, time.Millisecond)
	cancel()
	require.ErrorIs(t, <-errCh, context.Canceled)
}

func TestResolverErrorIsPreserved(t *testing.T) {
	errWant := errors.New("no gateway")
	client, err := New(Options{Hello: protocol.Hello{Protocol: protocol.ProtocolVersion}, Dialer: WSSDialer{}, Resolver: fakeResolver{err: errWant}, Config: &fakeConfig{}})
	require.NoError(t, err)
	_, err = client.runOnce(t.Context(), true)
	assert.ErrorIs(t, err, errWant)
}
