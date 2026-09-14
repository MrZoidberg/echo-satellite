package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/client"
	"github.com/MrZoidberg/echo-satellite/internal/device/update"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
	"github.com/MrZoidberg/echo-satellite/internal/release"
)

// simUpdateControls makes deployment outcomes repeatable without adding any
// release source or rollout policy to dotsim.
type simUpdateControls struct {
	interruptDownload bool
	digestMismatch    bool
	invalidSignature  bool
	insufficientSpace bool
	restartFailure    bool
	reconnectFailure  bool
	publicKey         ed25519.PublicKey
}

type simDeploymentManager struct {
	stateDir string
	controls simUpdateControls
	setupErr error
	restart  func()

	mu         sync.Mutex
	deployment string
	cancel     context.CancelFunc
	pending    update.Metadata
}

func newSimDeploymentManager(stateDir string, controls simUpdateControls) *simDeploymentManager {
	m := &simDeploymentManager{stateDir: stateDir, controls: controls, setupErr: initSimDeploymentState(stateDir), restart: func() {}}
	if m.setupErr == nil {
		m.pending = m.loadPending()
	}
	return m
}

func (m *simDeploymentManager) Offer(ctx context.Context, offer protocol.UpdateOffer, access client.UpdateAccess, report client.UpdateReporter) {
	if m.setupErr != nil {
		_ = report.ReportUpdateDecision(protocol.UpdateDecision{DeploymentID: offer.DeploymentID, Decision: protocol.UpdateDecisionRejected, Code: protocol.UpdateFailureInstallFailed, Detail: "simulator state unavailable"})
		return
	}
	m.mu.Lock()
	if m.deployment != "" {
		m.mu.Unlock()
		_ = report.ReportUpdateDecision(protocol.UpdateDecision{DeploymentID: offer.DeploymentID, Decision: protocol.UpdateDecisionRejected, Code: protocol.UpdateFailureBusy})
		return
	}
	m.deployment = offer.DeploymentID
	workCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()
	defer cancel()

	_ = report.ReportUpdateDecision(protocol.UpdateDecision{DeploymentID: offer.DeploymentID, Decision: protocol.UpdateDecisionAccepted})
	_ = report.ReportUpdateProgress(protocol.UpdateProgress{DeploymentID: offer.DeploymentID, Phase: protocol.PhaseDownloading, Percent: 0}, nil)
	_ = report.ReportUpdateProgress(protocol.UpdateProgress{DeploymentID: offer.DeploymentID, Phase: protocol.PhaseDownloading, Percent: 50}, nil)
	installer, err := m.installer(access)
	if err == nil {
		_ = report.ReportUpdateProgress(protocol.UpdateProgress{DeploymentID: offer.DeploymentID, Phase: protocol.PhaseVerifying, Percent: 0}, nil)
		result, installErr := installer.Install(workCtx, offer)
		if installErr == nil {
			m.mu.Lock()
			m.pending = result.Metadata
			m.mu.Unlock()
			err = nil
		} else {
			err = installErr
		}
	}
	if err != nil {
		if errors.Is(workCtx.Err(), context.Canceled) {
			m.finish(offer.DeploymentID)
			_ = report.ReportUpdateCancelled(protocol.UpdateCancellation{DeploymentID: offer.DeploymentID})
			return
		}
		_ = report.ReportUpdateFailed(protocol.UpdateFailure{DeploymentID: offer.DeploymentID, Code: simUpdateFailureCode(err), Detail: "simulated installation failed"})
		m.finish(offer.DeploymentID)
		return
	}
	_ = report.ReportUpdateProgress(protocol.UpdateProgress{DeploymentID: offer.DeploymentID, Phase: protocol.PhaseStaged, Percent: 100}, nil)
	if m.controls.restartFailure {
		_ = report.ReportUpdateFailed(protocol.UpdateFailure{DeploymentID: offer.DeploymentID, Code: protocol.UpdateFailureRestartFailed, Detail: "simulated restart failure"})
		m.finish(offer.DeploymentID)
		return
	}
	_ = report.ReportUpdateProgress(protocol.UpdateProgress{DeploymentID: offer.DeploymentID, Phase: protocol.PhaseRestarting, Percent: 100}, m.restart)
}

func (m *simDeploymentManager) Cancel(value protocol.UpdateCancellation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value.DeploymentID == m.deployment && m.cancel != nil {
		m.cancel()
	}
}

// Welcome simulates the replacement process reconnecting.
func (m *simDeploymentManager) Welcome(_ context.Context, report client.UpdateReporter) {
	m.mu.Lock()
	pending := m.pending
	m.mu.Unlock()
	if pending.PendingDeploymentID == "" {
		return
	}
	if err := report.ReportUpdateConfirmed(protocol.UpdateConfirmation{DeploymentID: pending.PendingDeploymentID, Version: pending.Version, BuildID: pending.BuildID}, func() {
		_ = update.ClearPendingDeployment(update.OSFileSystem{}, m.metadataPath(), true)
		m.mu.Lock()
		m.pending.PendingDeploymentID = ""
		m.mu.Unlock()
	}); err == nil {
		m.finish(pending.PendingDeploymentID)
	}
}

func (m *simDeploymentManager) loadPending() update.Metadata {
	raw, err := os.ReadFile(m.metadataPath())
	if err != nil {
		return update.Metadata{}
	}
	metadata, err := update.ParseMetadata(raw)
	if err != nil {
		return update.Metadata{}
	}
	return metadata
}

func (m *simDeploymentManager) pendingMetadata() update.Metadata {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending
}

func (m *simDeploymentManager) installer(access client.UpdateAccess) (*update.Installer, error) {
	installer, err := update.New(update.Config{
		Downloader:       simDownloader{base: update.HTTPDownloader{Client: access.HTTPClient, GatewayAuthority: access.GatewayAuthority, RequestHeaders: access.Headers}, controls: m.controls},
		FS:               update.OSFileSystem{},
		Space:            simSpace{insufficient: m.controls.insufficientSpace},
		Clock:            simClock{},
		Trust:            simTrust{policy: release.TrustPolicy{PublicKey: m.controls.publicKey}, invalid: m.controls.invalidSignature},
		Device:           release.Device{Architecture: "linux-amd64", Protocol: protocol.ProtocolVersion},
		TargetPath:       filepath.Join(m.stateDir, "agent", "echod"),
		MetadataPath:     m.metadataPath(),
		GatewayAuthority: access.GatewayAuthority,
		MaxArtifactSize:  256 << 20,
		DirectorySync:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("create simulator installer: %w", err)
	}
	return installer, nil
}

type simTrust struct {
	policy  release.TrustPolicy
	invalid bool
}

func (t simTrust) Check(manifest release.Manifest, signature []byte) error {
	if t.invalid {
		return release.ErrSignatureMismatch
	}
	if err := t.policy.Check(manifest, signature); err != nil {
		return fmt.Errorf("verify simulated release signature: %w", err)
	}
	return nil
}

func (m *simDeploymentManager) metadataPath() string {
	return filepath.Join(m.stateDir, "agent", "installed-release.json")
}

func (m *simDeploymentManager) finish(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deployment == id {
		m.deployment, m.cancel = "", nil
	}
}

func simUpdateFailureCode(err error) protocol.UpdateFailureCode {
	switch {
	case errors.Is(err, update.ErrInsufficientSpace):
		return protocol.UpdateFailureInsufficientSpace
	case errors.Is(err, release.ErrInvalidSignature), errors.Is(err, release.ErrSignatureMismatch), errors.Is(err, release.ErrNoPublicKey):
		return protocol.UpdateFailureSignatureInvalid
	case errors.Is(err, release.ErrSizeMismatch):
		return protocol.UpdateFailureSizeMismatch
	case errors.Is(err, release.ErrDigestMismatch):
		return protocol.UpdateFailureDigestMismatch
	case errors.Is(err, update.ErrDownloadFailed):
		return protocol.UpdateFailureDownloadFailed
	case errors.Is(err, update.ErrInvalidOffer), errors.Is(err, update.ErrOfferMismatch):
		return protocol.UpdateFailureInvalidOffer
	default:
		return protocol.UpdateFailureInstallFailed
	}
}

type simClock struct{}

func (simClock) Now() time.Time { return time.Unix(0, 0) }

type simSpace struct{ insufficient bool }

func (s simSpace) Available(string) (int64, error) {
	if s.insufficient {
		return 0, nil
	}
	return 1 << 30, nil
}

type simDownloader struct {
	base     update.Downloader
	controls simUpdateControls
}

func (d simDownloader) Fetch(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	body, err := d.base.Fetch(ctx, rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch simulated release resource: %w", err)
	}
	if d.controls.interruptDownload && simArtifactURL(rawURL) {
		return &simReadCloser{Reader: io.LimitReader(body, 1), closer: body}, nil
	}
	if d.controls.digestMismatch && simArtifactURL(rawURL) {
		return &simReadCloser{Reader: &simMutatingReader{Reader: body}, closer: body}, nil
	}
	return body, nil
}

func simArtifactURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && filepath.Base(parsed.Path) == "artifact"
}

type simReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *simReadCloser) Close() error {
	if err := r.closer.Close(); err != nil {
		return fmt.Errorf("close simulated release resource: %w", err)
	}
	return nil
}

type simMutatingReader struct {
	io.Reader
	mutated bool
}

func (r *simMutatingReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if n > 0 && !r.mutated {
		p[0] ^= 0xff
		r.mutated = true
	}
	if errors.Is(err, io.EOF) {
		return n, io.EOF
	}
	if err != nil {
		return n, fmt.Errorf("read simulated release artifact: %w", err)
	}
	return n, nil
}

var _ client.UpdateHandler = (*simDeploymentManager)(nil)

func initSimDeploymentState(stateDir string) error {
	if stateDir == "" {
		return errors.New("simulator update state directory is required")
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "agent"), 0o750); err != nil {
		return fmt.Errorf("create simulator update state: %w", err)
	}
	return nil
}
