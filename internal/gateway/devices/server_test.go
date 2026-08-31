package devices

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestServer_RejectsUnauthorizedBeforeUpgrade(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "https://gateway.test/device", http.NoBody)
	server.ServeHTTP(response, request)
	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.NotContains(t, response.Body.String(), "token")
}

func TestServer_RejectsMalformedHello(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	httpServer := httptest.NewTLSServer(server)
	defer httpServer.Close()
	conn, response, err := websocket.Dial(context.Background(), "wss"+httpServer.URL[len("https"):], &websocket.DialOptions{HTTPClient: httpServer.Client(), HTTPHeader: http.Header{"Authorization": []string{"Bearer device-token"}}})
	require.NoError(t, err)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	require.NoError(t, conn.Write(context.Background(), websocket.MessageText, []byte("not-json")))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err = conn.Read(ctx)
	require.Error(t, err)
}

func TestServer_WelcomeRegistersAndReplacesDuplicateSession(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	httpServer := httptest.NewTLSServer(server)
	defer httpServer.Close()
	first := dialHello(t, httpServer)
	defer func() { _ = first.Close(websocket.StatusNormalClosure, "done") }()
	metadata, found := server.Snapshot("dot-1")
	require.True(t, found)
	assert.Equal(t, "dot-1", metadata.DeviceID)
	assert.Contains(t, metadata.Capabilities, protocol.CapWakeLocal)

	second := dialHello(t, httpServer)
	defer func() { _ = second.Close(websocket.StatusNormalClosure, "done") }()
	readCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := first.Read(readCtx)
	require.Error(t, err)
	assert.Equal(t, []string{"dot-1"}, server.DeviceIDs())
}

func TestServer_TurnFramingIgnoresBinaryOutsideWindow(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	httpServer := httptest.NewTLSServer(server)
	defer httpServer.Close()
	conn := dialHello(t, httpServer)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	ctx := context.Background()
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte{1, 2}))
	require.NoError(t, write(t, conn, protocol.TypeTurnStart, "turn-1", protocol.TurnStart{Trigger: protocol.TriggerWake}))
	// A turn alone does not open the audio window; idle binary is discarded.
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte{1, 2}))
	require.NoError(t, write(t, conn, protocol.TypeAudioStart, "turn-1", protocol.AudioStart{SampleRate: 16000, Channels: 1, Format: protocol.AudioFormatPCMS16LE}))
	require.NoError(t, conn.Write(ctx, websocket.MessageBinary, []byte{1, 2}))
	require.NoError(t, write(t, conn, protocol.TypeAudioStop, "turn-1", protocol.AudioStop{Reason: protocol.AudioStopEndpointed}))
	require.Eventually(t, func() bool { metadata, found := server.Snapshot("dot-1"); return found && metadata.ActiveTurn == "" }, time.Second, 10*time.Millisecond)
}

func TestServer_Accepts64KiBPCMFrame(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	httpServer := httptest.NewTLSServer(server)
	defer httpServer.Close()
	conn := dialHello(t, httpServer)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	require.NoError(t, write(t, conn, protocol.TypeTurnStart, "turn-1", protocol.TurnStart{Trigger: protocol.TriggerWake}))
	require.NoError(t, write(t, conn, protocol.TypeAudioStart, "turn-1", protocol.AudioStart{SampleRate: 16000, Channels: 1, Format: protocol.AudioFormatPCMS16LE}))
	require.NoError(t, conn.Write(context.Background(), websocket.MessageBinary, make([]byte, 64<<10)))
	require.NoError(t, write(t, conn, protocol.TypeAudioStop, "turn-1", protocol.AudioStop{Reason: protocol.AudioStopEndpointed}))
	require.Eventually(t, func() bool { metadata, found := server.Snapshot("dot-1"); return found && metadata.ActiveTurn == "" }, time.Second, 10*time.Millisecond)
}

func TestServer_InvalidTurnSequencingClosesConnection(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	httpServer := httptest.NewTLSServer(server)
	defer httpServer.Close()
	conn := dialHello(t, httpServer)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	require.NoError(t, write(t, conn, protocol.TypeAudioStop, "turn-1", protocol.AudioStop{Reason: protocol.AudioStopEndpointed}))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _, err := conn.Read(ctx)
	require.Error(t, err)
}

func TestServer_PushConfig(t *testing.T) {
	t.Parallel()
	server := testServer(t)
	httpServer := httptest.NewTLSServer(server)
	defer httpServer.Close()
	conn := dialHello(t, httpServer)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "done") }()
	config := testConfig()
	config.Version = 2
	server.PushConfig(map[string]protocol.DeviceConfig{"dot-1": config})
	_, data, err := conn.Read(context.Background())
	require.NoError(t, err)
	env, err := protocol.Decode(data)
	require.NoError(t, err)
	assert.Equal(t, protocol.TypeConfig, env.Type)
	assert.Equal(t, "2", env.ID)
	var received protocol.DeviceConfig
	require.NoError(t, env.DecodePayload(&received))
	assert.Equal(t, config, received)
}

func TestBoundedFields_RedactsAndBounds(t *testing.T) {
	t.Parallel()
	fields := boundedFields(map[string]string{"token": "secret", "safe": string(make([]byte, 300))})
	assert.Equal(t, "[redacted]", fields["token"])
	assert.Len(t, fields["safe"], 256)
	assert.Equal(t, "short", truncate("short", 10))
}

func testServer(t *testing.T) *Server {
	t.Helper()
	server, err := New(Options{Token: []byte("device-token"), ServerID: "gateway", Config: func(string) protocol.DeviceConfig { return testConfig() }})
	require.NoError(t, err)
	return server
}

func dialHello(t *testing.T, httpServer *httptest.Server) *websocket.Conn {
	t.Helper()
	ctx := context.Background()
	conn, response, err := websocket.Dial(ctx, "wss"+httpServer.URL[len("https"):], &websocket.DialOptions{HTTPClient: httpServer.Client(), HTTPHeader: http.Header{"Authorization": []string{"Bearer device-token"}}})
	require.NoError(t, err)
	if response != nil && response.Body != nil {
		defer response.Body.Close()
	}
	require.NoError(t, write(t, conn, protocol.TypeHello, "", protocol.Hello{DeviceID: "dot-1", Protocol: protocol.ProtocolVersion, Capabilities: protocol.NewCapabilities(protocol.CapWakeLocal, protocol.CapCommandEndpointingLocal)}))
	_, data, err := conn.Read(ctx)
	require.NoError(t, err)
	env, err := protocol.Decode(data)
	require.NoError(t, err)
	assert.Equal(t, protocol.TypeWelcome, env.Type)
	return conn
}

func write(t *testing.T, conn *websocket.Conn, kind protocol.MessageType, id string, value any) error {
	t.Helper()
	data, err := protocol.Encode(kind, id, time.Now(), value)
	if err != nil {
		return fmt.Errorf("encode test control frame: %w", err)
	}
	if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		return fmt.Errorf("write test control frame: %w", err)
	}
	return nil
}

func testConfig() protocol.DeviceConfig {
	return protocol.DeviceConfig{Version: 1, Wake: protocol.WakeSettings{Engine: "wake", Model: "model", Threshold: 0.5, VADEnabled: true, VADThreshold: 0.5, VADLookbackMS: 1, PreRollMS: 1, MinIntervalMS: 1}, Endpointing: protocol.EndpointingConfig{SpeechThreshold: 0.5, SpeechOnsetMS: 1, TrailingSilenceMS: 1, NoSpeechTimeoutMS: 1, MaxTurnMS: 1}, Logs: protocol.LogSettings{ForwardLevel: protocol.LogLevelInfo}}
}
