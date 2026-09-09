package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

// UpdatePhase is the gateway-visible phase of an agent update on one device,
// as listed in docs/DESIGN.md 10.7. The device owns this state machine: the
// gateway observes it and never drives a device past a phase the device has
// not reported.
type UpdatePhase string

// Update phases.
const (
	PhaseIdle        UpdatePhase = "idle"
	PhaseAvailable   UpdatePhase = "available"
	PhaseQueued      UpdatePhase = "queued"
	PhaseDownloading UpdatePhase = "downloading"
	PhaseVerifying   UpdatePhase = "verifying"
	PhaseStaged      UpdatePhase = "staged"
	PhaseRestarting  UpdatePhase = "restarting"
	PhaseConfirmed   UpdatePhase = "confirmed"
	PhaseFailed      UpdatePhase = "failed"
	PhaseCancelled   UpdatePhase = "cancelled" //nolint:misspell // wire value fixed by docs/DESIGN.md 10.7
)

var allUpdatePhases = []UpdatePhase{
	PhaseIdle, PhaseAvailable, PhaseQueued, PhaseDownloading, PhaseVerifying,
	PhaseStaged, PhaseRestarting, PhaseConfirmed, PhaseFailed, PhaseCancelled,
}

// AllUpdatePhases returns every phase, in state-machine order.
func AllUpdatePhases() []UpdatePhase {
	out := make([]UpdatePhase, len(allUpdatePhases))
	copy(out, allUpdatePhases)
	return out
}

// String returns the wire representation of the phase.
func (p UpdatePhase) String() string { return string(p) }

// Valid reports whether the phase is defined by this protocol version.
func (p UpdatePhase) Valid() bool { return slices.Contains(allUpdatePhases, p) }

// Terminal reports whether no further phase follows without a new offer.
func (p UpdatePhase) Terminal() bool {
	switch p {
	case PhaseConfirmed, PhaseFailed, PhaseCancelled:
		return true
	default:
		return false
	}
}

// UpdateFailureCode is a stable, machine-readable reason for an unsuccessful deployment.
type UpdateFailureCode string

const (
	UpdateFailureBusy              UpdateFailureCode = "busy"
	UpdateFailureInvalidOffer      UpdateFailureCode = "invalid_offer"
	UpdateFailureIneligible        UpdateFailureCode = "ineligible"
	UpdateFailureInsufficientSpace UpdateFailureCode = "insufficient_space"
	UpdateFailureDownloadFailed    UpdateFailureCode = "download_failed"
	UpdateFailureSignatureInvalid  UpdateFailureCode = "signature_invalid"
	UpdateFailureSizeMismatch      UpdateFailureCode = "size_mismatch"
	UpdateFailureDigestMismatch    UpdateFailureCode = "digest_mismatch"
	UpdateFailureStageFailed       UpdateFailureCode = "stage_failed"
	UpdateFailureInstallFailed     UpdateFailureCode = "install_failed"
	UpdateFailureRestartFailed     UpdateFailureCode = "restart_failed"
)

func (c UpdateFailureCode) Valid() bool {
	switch c {
	case UpdateFailureBusy, UpdateFailureInvalidOffer, UpdateFailureIneligible, UpdateFailureInsufficientSpace, UpdateFailureDownloadFailed, UpdateFailureSignatureInvalid, UpdateFailureSizeMismatch, UpdateFailureDigestMismatch, UpdateFailureStageFailed, UpdateFailureInstallFailed, UpdateFailureRestartFailed:
		return true
	default:
		return false
	}
}

type UpdateDecisionKind string

const (
	UpdateDecisionAccepted UpdateDecisionKind = "accepted"
	UpdateDecisionRejected UpdateDecisionKind = "rejected"
)

func (d UpdateDecisionKind) Valid() bool {
	return d == UpdateDecisionAccepted || d == UpdateDecisionRejected
}

// UpdateOffer identifies the signed release and every HTTPS resource required to install it.
type UpdateOffer struct {
	DeploymentID string `json:"deployment_id"`
	Version      string `json:"version"`
	BuildID      string `json:"build_id"`
	ArtifactURL  string `json:"artifact_url"`
	ManifestURL  string `json:"manifest_url"`
	SignatureURL string `json:"signature_url"`
	Size         int64  `json:"size"`
	SHA256       string `json:"sha256"`
}

func (o UpdateOffer) Validate() error {
	if err := requireDeploymentID(o.DeploymentID); err != nil {
		return err
	}
	if strings.TrimSpace(o.Version) == "" || strings.TrimSpace(o.BuildID) == "" {
		return errors.New("protocol: update offer version and build ID are required")
	}
	for _, resource := range []struct{ name, value string }{{"artifact URL", o.ArtifactURL}, {"manifest URL", o.ManifestURL}, {"signature URL", o.SignatureURL}} {
		if err := requireHTTPSURL(resource.name, resource.value); err != nil {
			return err
		}
	}
	if o.Size <= 0 {
		return errors.New("protocol: update offer size must be positive")
	}
	digest, err := hex.DecodeString(o.SHA256)
	if err != nil || len(digest) != sha256.Size {
		return errors.New("protocol: update offer SHA-256 must be a 32-byte hexadecimal digest")
	}
	return nil
}
func (o *UpdateOffer) UnmarshalJSON(data []byte) error {
	type alias UpdateOffer
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("protocol: decode update offer: %w", err)
	}
	if err := requireExactObject(data, "update offer", "deployment_id", "version", "build_id", "artifact_url", "manifest_url", "signature_url", "size", "sha256"); err != nil {
		return err
	}
	*o = UpdateOffer(decoded)
	return o.Validate()
}

// UpdateDecision reports acceptance or rejection before installation begins.
type UpdateDecision struct {
	DeploymentID string             `json:"deployment_id"`
	Decision     UpdateDecisionKind `json:"decision"`
	Code         UpdateFailureCode  `json:"code"`
	Detail       string             `json:"detail"`
}

func (d UpdateDecision) Validate() error {
	if err := requireDeploymentID(d.DeploymentID); err != nil {
		return err
	}
	if !d.Decision.Valid() {
		return fmt.Errorf("protocol: invalid update decision %q", d.Decision)
	}
	if d.Decision == UpdateDecisionAccepted && d.Code != "" {
		return errors.New("protocol: accepted update decision cannot have a failure code")
	}
	if d.Decision == UpdateDecisionRejected && !d.Code.Valid() {
		return errors.New("protocol: rejected update decision requires a stable failure code")
	}
	return nil
}
func (d *UpdateDecision) UnmarshalJSON(data []byte) error {
	type alias UpdateDecision
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("protocol: decode update decision: %w", err)
	}
	if err := requireExactObject(data, "update decision", "deployment_id", "decision", "code", "detail"); err != nil {
		return err
	}
	*d = UpdateDecision(decoded)
	return d.Validate()
}

// UpdateProgress reports a non-terminal active phase and percent complete.
type UpdateProgress struct {
	DeploymentID string      `json:"deployment_id"`
	Phase        UpdatePhase `json:"phase"`
	Percent      int         `json:"percent"`
	Detail       string      `json:"detail"`
}

func (p UpdateProgress) Validate() error {
	if err := requireDeploymentID(p.DeploymentID); err != nil {
		return err
	}
	if !p.Phase.Valid() || p.Phase.Terminal() || p.Phase == PhaseIdle {
		return errors.New("protocol: update progress requires a non-terminal active phase")
	}
	if p.Percent < 0 || p.Percent > 100 {
		return errors.New("protocol: update progress percent must be between zero and 100")
	}
	return nil
}
func (p *UpdateProgress) UnmarshalJSON(data []byte) error {
	type alias UpdateProgress
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("protocol: decode update progress: %w", err)
	}
	if err := requireExactObject(data, "update progress", "deployment_id", "phase", "percent", "detail"); err != nil {
		return err
	}
	*p = UpdateProgress(decoded)
	return p.Validate()
}

type UpdateConfirmation struct {
	DeploymentID string `json:"deployment_id"`
	Version      string `json:"version"`
	BuildID      string `json:"build_id"`
}

func (c UpdateConfirmation) Validate() error {
	if err := requireDeploymentID(c.DeploymentID); err != nil {
		return err
	}
	if strings.TrimSpace(c.Version) == "" || strings.TrimSpace(c.BuildID) == "" {
		return errors.New("protocol: update confirmation version and build ID are required")
	}
	return nil
}
func (c *UpdateConfirmation) UnmarshalJSON(data []byte) error {
	type alias UpdateConfirmation
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("protocol: decode update confirmation: %w", err)
	}
	if err := requireExactObject(data, "update confirmation", "deployment_id", "version", "build_id"); err != nil {
		return err
	}
	*c = UpdateConfirmation(decoded)
	return c.Validate()
}

type UpdateCancellation struct {
	DeploymentID string `json:"deployment_id"`
	Detail       string `json:"detail"`
}

func (c UpdateCancellation) Validate() error { return requireDeploymentID(c.DeploymentID) }
func (c *UpdateCancellation) UnmarshalJSON(data []byte) error {
	type alias UpdateCancellation
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("protocol: decode update cancellation: %w", err)
	}
	if err := requireExactObject(data, "update cancellation", "deployment_id", "detail"); err != nil {
		return err
	}
	*c = UpdateCancellation(decoded)
	return c.Validate()
}

type UpdateFailure struct {
	DeploymentID string            `json:"deployment_id"`
	Code         UpdateFailureCode `json:"code"`
	Detail       string            `json:"detail"`
}

func (f UpdateFailure) Validate() error {
	if err := requireDeploymentID(f.DeploymentID); err != nil {
		return err
	}
	if !f.Code.Valid() {
		return fmt.Errorf("protocol: invalid update failure code %q", f.Code)
	}
	return nil
}
func (f *UpdateFailure) UnmarshalJSON(data []byte) error {
	type alias UpdateFailure
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return fmt.Errorf("protocol: decode update failure: %w", err)
	}
	if err := requireExactObject(data, "update failure", "deployment_id", "code", "detail"); err != nil {
		return err
	}
	*f = UpdateFailure(decoded)
	return f.Validate()
}

func requireDeploymentID(id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("protocol: deployment ID is required")
	}
	return nil
}
func requireHTTPSURL(name, raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("protocol: update offer %s must be an absolute HTTPS URL", name)
	}
	return nil
}
func requireExactObject(data []byte, name string, keys ...string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("protocol: decode %s: %w", name, err)
	}
	if err := requireObjectKeys(object, name, keys...); err != nil {
		return err
	}
	for key := range object {
		if !slices.Contains(keys, key) {
			return fmt.Errorf("protocol: %s has unknown field %q", name, key)
		}
	}
	return nil
}

// ParseUpdatePhase converts a wire value into an UpdatePhase.
func ParseUpdatePhase(s string) (UpdatePhase, error) {
	p := UpdatePhase(s)
	if !p.Valid() {
		return "", fmt.Errorf("protocol: unknown update phase %q", s)
	}
	return p, nil
}
