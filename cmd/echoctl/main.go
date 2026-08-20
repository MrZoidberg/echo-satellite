// Command echoctl provisions and diagnoses Echo Satellite devices.
//
// Milestone 0 exposes the commands whose subsystem already exists: version, and
// release verification, which applies the same integrity and signature checks a
// gateway or a device applies before staging a release.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/MrZoidberg/echo-satellite/internal/release"
)

// revision is set at link time by the build.
var revision = "unknown"

func main() {
	o, command, err := parseArgs(os.Args[1:])
	if err != nil {
		if isHelpRequest(err) {
			return
		}
		fmt.Fprintf(os.Stderr, "echoctl: %v\n", err)
		os.Exit(1)
	}

	if err := dispatch(os.Stdout, command, o); err != nil {
		fmt.Fprintf(os.Stderr, "echoctl: %v\n", err)
		os.Exit(1)
	}
}

func dispatch(w io.Writer, command string, o opts) error {
	switch command {
	case "version":
		return writeReport(w, []string{"version: " + revision})
	case "release verify":
		return verifyRelease(w, o.Release.Verify)
	case "mic record":
		return micRecord(w, o.Mic.Record)
	case "speaker test":
		return speakerTest(w, o.Speaker.Test)
	case "led test":
		return ledTest(w, o.LED.Test)
	case "buttons test":
		return buttonsTest(w, o.Buttons.Test)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

// verifyRelease checks a release bundle: the manifest parses and validates, the
// manifest signature verifies, and the artifact matches its declared size and
// digest. An unsigned bundle fails unless the development escape hatch is on.
//
// The report is written only after every check passed, so a failed verification
// never prints a line that reads like partial success.
func verifyRelease(w io.Writer, c verifyCommand) error {
	manifestData, err := os.ReadFile(c.Manifest)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	manifest, err := release.ParseManifest(manifestData)
	if err != nil {
		return fmt.Errorf("manifest %s: %w", c.Manifest, err)
	}

	policy, err := trustPolicy(c)
	if err != nil {
		return err
	}
	sig, err := readSignature(c.Sig)
	if err != nil {
		return err
	}
	if err = policy.Check(manifest, sig); err != nil {
		return fmt.Errorf("trust check: %w", err)
	}

	report := make([]string, 0, 5)
	for _, note := range policy.StatusNotes() {
		report = append(report, "warning:   "+note)
	}
	report = append(report,
		fmt.Sprintf("manifest:  version %s build %s arch %s", manifest.Version, manifest.BuildID, manifest.Architecture),
		"signature: "+signatureStatus(c, policy))

	if c.Artifact != "" {
		if artErr := verifyArtifactFile(c.Artifact, manifest); artErr != nil {
			return artErr
		}
		report = append(report, fmt.Sprintf("artifact:  ok, %d bytes, sha256 matches", manifest.Size))
	}

	report = append(report, "result:    release bundle accepted")
	return writeReport(w, report)
}

func trustPolicy(c verifyCommand) (release.TrustPolicy, error) {
	policy := release.TrustPolicy{AllowUnsignedDevBuilds: c.AllowUnsigned}
	if c.PubKey == "" {
		return policy, nil
	}

	encoded, err := os.ReadFile(c.PubKey)
	if err != nil {
		return release.TrustPolicy{}, fmt.Errorf("read public key: %w", err)
	}
	pub, err := release.ParsePublicKey(string(encoded))
	if err != nil {
		return release.TrustPolicy{}, fmt.Errorf("public key %s: %w", c.PubKey, err)
	}
	policy.PublicKey = pub
	return policy, nil
}

func readSignature(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}
	encoded, err := os.ReadFile(path) //nolint:gosec // G304: the operator names the signature file to read
	if err != nil {
		return nil, fmt.Errorf("read signature: %w", err)
	}
	sig, err := release.DecodeSignature(string(encoded))
	if err != nil {
		return nil, fmt.Errorf("signature %s: %w", path, err)
	}
	return sig, nil
}

func verifyArtifactFile(path string, manifest release.Manifest) error {
	f, err := os.Open(path) //nolint:gosec // G304: the operator names the artifact to verify
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err = release.VerifyArtifact(f, manifest); err != nil {
		return fmt.Errorf("artifact %s: %w", path, err)
	}
	return nil
}

func signatureStatus(c verifyCommand, policy release.TrustPolicy) string {
	if c.Sig == "" {
		return "absent, accepted because unsigned development builds are allowed"
	}
	if len(policy.PublicKey) > 0 {
		return "verified against " + c.PubKey
	}
	return "verified against the key built into this binary"
}

func writeReport(w io.Writer, lines []string) error {
	if _, err := io.WriteString(w, strings.Join(lines, "\n")+"\n"); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
