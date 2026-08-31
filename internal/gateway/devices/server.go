// Package devices implements authenticated device WebSocket sessions.
package devices

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/MrZoidberg/echo-satellite/internal/gateway/turns"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

const maxFrameBytes = 64 << 10

// Config selects the complete desired configuration for a connected device.
type Config func(deviceID string) protocol.DeviceConfig

// Options configures a Server.
type Options struct {
	Token    []byte
	ServerID string
	Config   Config
	Turns    turns.Receiver
	Logger   *slog.Logger
	Now      func() time.Time
}

// Server is an HTTP handler and a concurrency-safe device registry.
type Server struct {
	token    []byte
	serverID string
	config   Config
	turns    turns.Receiver
	logger   *slog.Logger
	now      func() time.Time

	mu       sync.RWMutex
	sessions map[string]*session
}

// Metadata is a snapshot of a registered device. It deliberately excludes
// credentials and received PCM.
type Metadata struct {
	DeviceID     string
	LastSeen     time.Time
	Capabilities protocol.Capabilities
	WakeConfig   protocol.WakeConfig
	ConfigResult protocol.ConfigResult
	ActiveTurn   string
}

type session struct {
	server   *Server
	conn     *websocket.Conn
	metadata Metadata
	mu       sync.Mutex
	active   *turns.Active
	done     chan struct{}
	once     sync.Once
}

// New validates server options.
func New(opts Options) (*Server, error) {
	if len(opts.Token) == 0 || strings.TrimSpace(opts.ServerID) == "" || opts.Config == nil {
		return nil, errors.New("gateway devices: token, server ID, and config are required")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Server{token: append([]byte(nil), opts.Token...), serverID: opts.ServerID, config: opts.Config, turns: opts.Turns, logger: opts.Logger, now: opts.Now, sessions: make(map[string]*session)}, nil
}

// ServeHTTP authenticates before upgrade, so unauthenticated callers never
// acquire a WebSocket or learn why a token did not match.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r.Header.Get("Authorization")) {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(maxFrameBytes)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "session ended") }()
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	type_, data, err := conn.Read(ctx)
	if err != nil || type_ != websocket.MessageText || len(data) > maxFrameBytes {
		s.closeProtocol(conn, "hello required")
		return
	}
	env, err := protocol.Decode(data)
	if err != nil || env.Type != protocol.TypeHello {
		s.closeProtocol(conn, "hello required")
		return
	}
	var hello protocol.Hello
	if err = env.DecodePayload(&hello); err != nil || !validHello(hello) {
		s.closeProtocol(conn, "invalid hello")
		return
	}

	session := &session{server: s, conn: conn, metadata: Metadata{DeviceID: hello.DeviceID, LastSeen: s.now(), Capabilities: append(protocol.Capabilities(nil), hello.Capabilities...), WakeConfig: hello.WakeConfig}, done: make(chan struct{})}
	config := s.config(hello.DeviceID)
	if err = session.write(ctx, protocol.TypeWelcome, "", protocol.Welcome{ServerID: s.serverID, Protocol: protocol.ProtocolVersion, Config: config}); err != nil {
		return
	}
	s.register(session)
	defer s.unregister(session)
	sessionCtx, sessionCancel := context.WithCancel(context.Background())
	defer sessionCancel()
	for {
		type_, data, err = conn.Read(sessionCtx)
		if err != nil {
			return
		}
		if len(data) > maxFrameBytes {
			s.closeProtocol(conn, "frame too large")
			return
		}
		if type_ == websocket.MessageBinary {
			if !session.binary(data) {
				s.closeProtocol(conn, "invalid PCM frame")
				return
			}
			continue
		}
		if type_ != websocket.MessageText {
			s.closeProtocol(conn, "unsupported frame")
			return
		}
		if !session.control(sessionCtx, data) {
			return
		}
	}
}

// DeviceIDs returns a stable point-in-time list for config reload callers.
func (s *Server) DeviceIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Snapshot returns non-secret metadata for one currently connected device.
func (s *Server) Snapshot(deviceID string) (Metadata, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.sessions[deviceID]
	if !ok {
		return Metadata{}, false
	}
	return value.snapshot(), true
}

// PushConfig sends a validated config to each currently connected recipient.
func (s *Server) PushConfig(configs map[string]protocol.DeviceConfig) {
	s.mu.RLock()
	sessions := make([]*session, 0, len(configs))
	for id := range configs {
		if current := s.sessions[id]; current != nil {
			sessions = append(sessions, current)
		}
	}
	s.mu.RUnlock()
	for _, current := range sessions {
		config := configs[current.snapshot().DeviceID]
		writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := current.write(writeCtx, protocol.TypeConfig, strconv.FormatUint(config.Version, 10), config)
		cancel()
		if err != nil {
			current.close(websocket.StatusGoingAway, "config delivery failed")
		}
	}
}

// Close gracefully disconnects all active sessions.
func (s *Server) Close() {
	s.mu.RLock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, value := range s.sessions {
		sessions = append(sessions, value)
	}
	s.mu.RUnlock()
	for _, value := range sessions {
		value.close(websocket.StatusGoingAway, "gateway stopping")
	}
}

func (s *Server) authorized(header string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	supplied := []byte(strings.TrimPrefix(header, prefix))
	return len(supplied) == len(s.token) && subtle.ConstantTimeCompare(supplied, s.token) == 1
}
func (s *Server) register(next *session) {
	s.mu.Lock()
	old := s.sessions[next.metadata.DeviceID]
	s.sessions[next.metadata.DeviceID] = next
	s.mu.Unlock()
	if old != nil {
		old.close(websocket.StatusPolicyViolation, "replaced by newer session")
	}
}
func (s *Server) unregister(value *session) {
	s.mu.Lock()
	if s.sessions[value.metadata.DeviceID] == value {
		delete(s.sessions, value.metadata.DeviceID)
	}
	s.mu.Unlock()
	value.close(websocket.StatusNormalClosure, "session ended")
}
func (s *Server) closeProtocol(conn *websocket.Conn, message string) {
	_ = conn.Close(websocket.StatusProtocolError, message)
}

func validHello(value protocol.Hello) bool {
	return strings.TrimSpace(value.DeviceID) != "" && value.Protocol == protocol.ProtocolVersion && value.Capabilities.Has(protocol.CapWakeLocal) && value.Capabilities.Has(protocol.CapCommandEndpointingLocal)
}
func (s *session) close(code websocket.StatusCode, reason string) {
	s.once.Do(func() {
		close(s.done)
		_ = s.conn.Close(code, reason)
		s.mu.Lock()
		if s.active != nil {
			s.active.Abort()
			s.active = nil
		}
		s.mu.Unlock()
	})
}
func (s *session) snapshot() Metadata { s.mu.Lock(); defer s.mu.Unlock(); return s.metadata }
func (s *session) write(ctx context.Context, kind protocol.MessageType, id string, payload any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := protocol.Encode(kind, id, s.server.now(), payload)
	if err != nil {
		return fmt.Errorf("encode control frame: %w", err)
	}
	if len(data) > maxFrameBytes {
		return errors.New("gateway devices: control frame exceeds 64 KiB")
	}
	if err := s.conn.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("write control frame: %w", err)
	}
	return nil
}

func (s *session) binary(data []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata.LastSeen = s.server.now()
	if s.active == nil {
		return true
	}
	if !s.active.AudioOpen() {
		return true
	}
	if err := s.server.turns.Write(s.active, data); err != nil {
		s.server.logger.Warn("discard invalid device PCM", "device_id", s.metadata.DeviceID, "error", err)
		s.active.Abort()
		s.active = nil
		s.metadata.ActiveTurn = ""
		return false
	}
	return true
}
func (s *session) control(ctx context.Context, data []byte) bool {
	env, err := protocol.Decode(data)
	if err != nil {
		s.server.closeProtocol(s.conn, "invalid control frame")
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metadata.LastSeen = s.server.now()
	return s.handleControlLocked(ctx, env)
}

func (s *session) handleControlLocked(ctx context.Context, env protocol.Envelope) bool {
	switch env.Type {
	case protocol.TypeTurnStart:
		var start protocol.TurnStart
		if err := env.DecodePayload(&start); err != nil || s.active != nil {
			s.server.closeProtocol(s.conn, "invalid turn start")
			return false
		}
		active, err := s.server.turns.Begin(env.ID, start, s.server.now())
		if err != nil {
			s.server.closeProtocol(s.conn, "invalid turn start")
			return false
		}
		s.active = active
		s.metadata.ActiveTurn = env.ID
	case protocol.TypeAudioStart:
		var start protocol.AudioStart
		if err := env.DecodePayload(&start); err != nil || s.active == nil || s.server.turns.StartAudio(s.active, env.ID, start) != nil {
			s.server.closeProtocol(s.conn, "invalid audio start")
			return false
		}
	case protocol.TypeAudioStop:
		var stop protocol.AudioStop
		if err := env.DecodePayload(&stop); err != nil || s.active == nil {
			s.server.closeProtocol(s.conn, "invalid audio stop")
			return false
		}
		if _, err := s.server.turns.Stop(s.active, env.ID, stop); err != nil {
			s.server.closeProtocol(s.conn, "invalid audio stop")
			return false
		}
		s.active, s.metadata.ActiveTurn = nil, ""
	case protocol.TypeConfigResult:
		var result protocol.ConfigResult
		if err := env.DecodePayload(&result); err != nil || result.Validate() != nil {
			s.server.closeProtocol(s.conn, "invalid config result")
			return false
		}
		s.metadata.ConfigResult = result
	case protocol.TypeLog:
		var record protocol.LogRecord
		if err := env.DecodePayload(&record); err != nil || record.Validate() != nil {
			s.server.closeProtocol(s.conn, "invalid log record")
			return false
		}
		s.server.logger.Log(ctx, slog.LevelInfo, "device log", "device_id", s.metadata.DeviceID, "level", record.Level, "message", truncate(record.Message, 256), "fields", boundedFields(record.Fields))
	case protocol.TypePing:
		go func() { _ = s.write(ctx, protocol.TypePong, "", nil) }()
	default:
		// Unknown and future messages are explicitly forward-compatible.
	}
	return true
}

func boundedFields(input map[string]string) map[string]string {
	const limit = 64
	output := make(map[string]string, min(len(input), limit))
	for key, value := range input {
		if len(output) == limit {
			break
		}
		lowered := strings.ToLower(key)
		if strings.Contains(lowered, "token") || strings.Contains(lowered, "secret") || strings.Contains(lowered, "password") || strings.Contains(lowered, "cookie") || strings.Contains(lowered, "api_key") {
			output[key] = "[redacted]"
			continue
		}
		output[truncate(key, 128)] = truncate(value, 256)
	}
	return output
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
