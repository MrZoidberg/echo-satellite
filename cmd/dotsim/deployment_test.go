package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/client"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
	"github.com/MrZoidberg/echo-satellite/internal/release"
)

func TestSimDeploymentLifecycleAndSignedDowngrade(t *testing.T) {
	server := newSimReleaseServer(t, false)
	manager := newSimDeploymentManager(t.TempDir(), simUpdateControls{publicKey: server.publicKey})
	reporter := &simReporter{}
	manager.Offer(t.Context(), server.offer("deploy-new", "2.0.0", "build-new"), server.access(), reporter)
	assert.Equal(t, []protocol.UpdatePhase{protocol.PhaseDownloading, protocol.PhaseDownloading, protocol.PhaseVerifying, protocol.PhaseStaged, protocol.PhaseRestarting}, reporter.phases())
	assert.Equal(t, []int{0, 50, 0, 100, 100}, reporter.percents())
	manager = newSimDeploymentManager(manager.stateDir, simUpdateControls{publicKey: server.publicKey})
	manager.Welcome(t.Context(), reporter)
	assert.Equal(t, []string{"accepted", "confirmed"}, reporter.events())

	// A rollback is a new, signed deployment; lower semantic version is not a
	// special path and no prior executable is selected.
	reporter = &simReporter{}
	manager.Offer(t.Context(), server.offer("deploy-old", "1.0.0", "build-old"), server.access(), reporter)
	manager = newSimDeploymentManager(manager.stateDir, simUpdateControls{publicKey: server.publicKey})
	manager.Welcome(t.Context(), reporter)
	assert.Equal(t, []string{"accepted", "confirmed"}, reporter.events())
}

func TestSimDeploymentDeterministicTerminalOutcomes(t *testing.T) {
	for _, test := range []struct {
		name     string
		controls simUpdateControls
		badSig   bool
		want     string
	}{
		{name: "interrupted download", controls: simUpdateControls{interruptDownload: true}, want: string(protocol.UpdateFailureSizeMismatch)},
		{name: "invalid signature", controls: simUpdateControls{invalidSignature: true}, want: string(protocol.UpdateFailureSignatureInvalid)},
		{name: "digest mismatch", controls: simUpdateControls{digestMismatch: true}, want: string(protocol.UpdateFailureDigestMismatch)},
		{name: "insufficient space", controls: simUpdateControls{insufficientSpace: true}, want: string(protocol.UpdateFailureInsufficientSpace)},
		{name: "restart failure", controls: simUpdateControls{restartFailure: true}, want: string(protocol.UpdateFailureRestartFailed)},
		{name: "reconnect failure", controls: simUpdateControls{reconnectFailure: true}, want: string(protocol.UpdateFailureRestartFailed)},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newSimReleaseServer(t, test.badSig)
			test.controls.publicKey = server.publicKey
			manager := newSimDeploymentManager(t.TempDir(), test.controls)
			reporter := &simReporter{}
			manager.Offer(t.Context(), server.offer("deploy-1", "2.0.0", "build-1"), server.access(), reporter)
			if test.controls.reconnectFailure {
				assert.Equal(t, []string{"accepted"}, reporter.events())
				return
			}
			assert.Equal(t, []string{"accepted", "failed:" + test.want}, reporter.events())
		})
	}
}

func TestSimDeploymentCancellation(t *testing.T) {
	server := newSimReleaseServer(t, false)
	manager := newSimDeploymentManager(t.TempDir(), simUpdateControls{publicKey: server.publicKey})
	reporter := &simReporter{onDownloading: func() { manager.Cancel(protocol.UpdateCancellation{DeploymentID: "deploy-1"}) }}
	manager.Offer(t.Context(), server.offer("deploy-1", "2.0.0", "build-1"), server.access(), reporter)
	assert.Equal(t, []string{"accepted", string(protocol.PhaseCancelled)}, reporter.events())
}

type simReporter struct {
	mu            sync.Mutex
	event         []string
	progress      []protocol.UpdatePhase
	percent       []int
	onDownloading func()
}

func (r *simReporter) ReportUpdateDecision(value protocol.UpdateDecision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.event = append(r.event, string(value.Decision))
	return nil
}
func (r *simReporter) ReportUpdateProgress(value protocol.UpdateProgress, done func()) error {
	r.mu.Lock()
	r.progress = append(r.progress, value.Phase)
	r.percent = append(r.percent, value.Percent)
	onDownloading := r.onDownloading
	r.mu.Unlock()
	if value.Phase == protocol.PhaseDownloading && onDownloading != nil {
		onDownloading()
	}
	if done != nil {
		done()
	}
	return nil
}
func (r *simReporter) percents() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.percent...)
}
func (r *simReporter) ReportUpdateConfirmed(protocol.UpdateConfirmation, func()) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.event = append(r.event, "confirmed")
	return nil
}
func (r *simReporter) ReportUpdateCancelled(protocol.UpdateCancellation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.event = append(r.event, string(protocol.PhaseCancelled))
	return nil
}
func (r *simReporter) ReportUpdateFailed(value protocol.UpdateFailure) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.event = append(r.event, "failed:"+string(value.Code))
	return nil
}
func (r *simReporter) events() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.event...)
}
func (r *simReporter) phases() []protocol.UpdatePhase {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]protocol.UpdatePhase(nil), r.progress...)
}

type simReleaseServer struct {
	server    *httptest.Server
	publicKey ed25519.PublicKey
	artifact  []byte
}

func newSimReleaseServer(t *testing.T, badSignature bool) *simReleaseServer {
	t.Helper()
	pub, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	artifact := []byte("simulated signed agent artifact")
	digest := sha256.Sum256(artifact)
	manifest := release.Manifest{Schema: release.SchemaVersion, Version: "placeholder", BuildID: "placeholder", Architecture: "linux-amd64", Size: int64(len(artifact)), SHA256: hex.EncodeToString(digest[:]), ProtocolMin: protocol.ProtocolVersion, ProtocolMax: protocol.ProtocolVersion, ReleasedAt: time.Unix(0, 0).UTC()}
	// The server rebuilds and signs a manifest for each offer, so deployments can
	// model both upgrades and signed downgrades without any release persistence.
	response := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		version, build := r.URL.Query().Get("version"), r.URL.Query().Get("build")
		m := manifest
		m.Version, m.BuildID = version, build
		signature, signErr := release.Sign(private, m)
		if signErr != nil {
			http.Error(w, "sign manifest", http.StatusInternalServerError)
			return
		}
		if badSignature {
			signature[0] ^= 0xff
		}
		switch r.URL.Path {
		case "/artifact":
			_, _ = w.Write(artifact)
		case "/manifest":
			raw, marshalErr := json.Marshal(m)
			if marshalErr != nil {
				http.Error(w, "marshal manifest", http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(raw)
		case "/signature":
			_, _ = w.Write(signature)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(response.Close)
	return &simReleaseServer{server: response, publicKey: pub, artifact: artifact}
}

func (s *simReleaseServer) offer(deploymentID, version, build string) protocol.UpdateOffer {
	base, err := url.Parse(s.server.URL)
	if err != nil {
		panic(err)
	}
	urlFor := func(path string) string {
		u := *base
		u.Path = path
		q := u.Query()
		q.Set("version", version)
		q.Set("build", build)
		u.RawQuery = q.Encode()
		return u.String()
	}
	digest := sha256.Sum256(s.artifact)
	return protocol.UpdateOffer{DeploymentID: deploymentID, Version: version, BuildID: build, ArtifactURL: urlFor("/artifact"), ManifestURL: urlFor("/manifest"), SignatureURL: urlFor("/signature"), Size: int64(len(s.artifact)), SHA256: hex.EncodeToString(digest[:])}
}

func (s *simReleaseServer) access() client.UpdateAccess {
	base, err := url.Parse(s.server.URL)
	if err != nil {
		panic(err)
	}
	return client.UpdateAccess{HTTPClient: s.server.Client(), GatewayAuthority: base.Host, Headers: http.Header{"Authorization": []string{"Bearer test-token"}}}
}

var _ client.UpdateReporter = (*simReporter)(nil)
