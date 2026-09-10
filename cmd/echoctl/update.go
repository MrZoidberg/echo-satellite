package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/device/update"
	"github.com/MrZoidberg/echo-satellite/internal/protocol"
	"github.com/MrZoidberg/echo-satellite/internal/release"
)

const (
	legacyServiceDir = "/sbin/.core/img/.core/service.d"
	launcherName     = "echo-satellite.sh"
	directStartName  = "echod"
	launcherMarker   = "# echo-satellite-launcher-v1"
	localAuthority   = "local.install"
)

type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // ADB path is explicitly supplied by the operator.
	if err != nil {
		return out, fmt.Errorf("run %s: %w", name, err)
	}
	return out, nil
}

func updateBootstrap(w io.Writer, c updateBootstrapCommand) error {
	return bootstrapWithRunner(context.Background(), w, c, execRunner{})
}

func bootstrapWithRunner(ctx context.Context, w io.Writer, c updateBootstrapCommand, runner commandRunner) error {
	launcher, err := os.ReadFile(c.Launcher)
	if err != nil {
		return fmt.Errorf("read launcher: %w", err)
	}
	if !bytes.Contains(launcher, []byte(launcherMarker)) {
		return errors.New("launcher is not an Echo Satellite launcher payload")
	}
	if _, statErr := os.Stat(c.Agent); statErr != nil {
		return fmt.Errorf("stat agent: %w", statErr)
	}

	const remoteLauncher = "/data/local/tmp/echo-satellite-launcher.part"
	const remoteAgent = "/data/local/tmp/echod.part"
	const remoteScript = "/data/local/tmp/echo-satellite-bootstrap.sh"
	if _, pushErr := runner.Run(ctx, c.ADB, "-s", c.Serial, "push", c.Launcher, remoteLauncher); pushErr != nil {
		return fmt.Errorf("push launcher: %w", pushErr)
	}
	if _, pushErr := runner.Run(ctx, c.ADB, "-s", c.Serial, "push", c.Agent, remoteAgent); pushErr != nil {
		return fmt.Errorf("push agent: %w", pushErr)
	}
	localScript, err := os.CreateTemp("", "echo-satellite-bootstrap-*.sh")
	if err != nil {
		return fmt.Errorf("create bootstrap script: %w", err)
	}
	localScriptPath := localScript.Name()
	defer func() { _ = os.Remove(localScriptPath) }()
	if _, err := localScript.WriteString("#!/system/bin/sh\ntrap 'rm -f \"$0\"' EXIT\n" + bootstrapScriptWithComparator(remoteLauncher, remoteAgent, "/data/adb/magisk/busybox cmp")); err != nil {
		_ = localScript.Close()
		return fmt.Errorf("write bootstrap script: %w", err)
	}
	if err := localScript.Close(); err != nil {
		return fmt.Errorf("close bootstrap script: %w", err)
	}
	if _, err := runner.Run(ctx, c.ADB, "-s", c.Serial, "push", localScriptPath, remoteScript); err != nil {
		return fmt.Errorf("push bootstrap script: %w", err)
	}
	if _, err := runner.Run(ctx, c.ADB, "-s", c.Serial, "shell", "chmod", "0700", remoteScript); err != nil {
		return fmt.Errorf("make bootstrap script executable: %w", err)
	}
	if _, err := runner.Run(ctx, c.ADB, "-s", c.Serial, "shell", "su", "-c", remoteScript); err != nil {
		return fmt.Errorf("install bootstrap payloads: %w", err)
	}
	return writeReport(w, []string{"result: bootstrap installed launcher and known-good agent"})
}

func bootstrapScript(remoteLauncher, remoteAgent string) string {
	return bootstrapScriptWithComparator(remoteLauncher, remoteAgent, "cmp")
}

func bootstrapScriptWithComparator(remoteLauncher, remoteAgent, comparator string) string {
	// Every path is fixed by this binary. The remote shell receives one quoted
	// su -c argument, so its redirections remain privileged.
	return `set -eu
service_dir="` + legacyServiceDir + `"
launcher="$service_dir/` + launcherName + `"
backup="$launcher.echo-satellite-backup"
direct_start="$service_dir/` + directStartName + `"
direct_backup="$direct_start.echo-satellite-backup"
test -d "$service_dir" || { echo "unsupported Magisk service layout" >&2; exit 64; }
test -f "` + remoteLauncher + `" || { echo "missing staged launcher" >&2; exit 65; }
test -f "` + remoteAgent + `" || { echo "missing staged agent" >&2; exit 65; }
if test -e "$launcher"; then
  if ` + comparator + ` -s "` + remoteLauncher + `" "$launcher"; then
    launcher_action=unchanged
  elif test "$(sed -n '2p' "$launcher")" = '` + launcherMarker + `'; then
    test ! -e "$backup" || { echo "existing launcher backup would be overwritten" >&2; exit 66; }
    launcher_action=replace
  else
    echo "unrecognized conflicting launcher" >&2
    exit 67
  fi
else
  launcher_action=create
fi
if test -e "$direct_start"; then
  test "$(sed -n '2p' "$direct_start")" = '` + launcherMarker + `' || { echo "unrecognized conflicting direct-start hook" >&2; exit 68; }
  test ! -e "$direct_backup" || { echo "existing direct-start backup would be overwritten" >&2; exit 69; }
  direct_action=backup
else
  direct_action=unchanged
fi
if test "$launcher_action" != unchanged; then
  cp "` + remoteLauncher + `" "$launcher.echo-satellite-new"
  chmod 0755 "$launcher.echo-satellite-new"
fi
if test "$launcher_action" = replace; then
  cp "$launcher" "$backup.echo-satellite-new"
  chmod 0755 "$backup.echo-satellite-new"
  mv "$backup.echo-satellite-new" "$backup"
fi
if test "$direct_action" = backup; then
  mv "$direct_start" "$direct_backup"
fi
case "$launcher_action" in
  unchanged) rm -f "` + remoteLauncher + `" ;;
  replace|create) mv "$launcher.echo-satellite-new" "$launcher"; rm -f "` + remoteLauncher + `" ;;
esac
if ! test -d /data/local/etc/echo-satellite; then
  mkdir -p /data/local/etc/echo-satellite
  chmod 0700 /data/local/etc/echo-satellite
fi
mkdir -p /data/local/bin
if test -e /data/local/bin/echod && ` + comparator + ` -s "` + remoteAgent + `" /data/local/bin/echod; then
  rm -f "` + remoteAgent + `"
else
  chmod 0755 "` + remoteAgent + `"
  mv "` + remoteAgent + `" /data/local/bin/echod
fi
`
}

func updateInstall(w io.Writer, c updateInstallCommand) error {
	if err := os.MkdirAll(filepath.Dir(c.MetadataPath), 0o700); err != nil {
		return fmt.Errorf("create installed-release metadata directory: %w", err)
	}
	manifestData, err := os.ReadFile(c.Manifest)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	manifest, err := release.ParseManifest(manifestData)
	if err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	signature, err := readSignature(c.Sig)
	if err != nil {
		return err
	}
	policy, err := trustPolicy(verifyCommand{PubKey: c.PubKey, AllowUnsigned: c.AllowUnsigned})
	if err != nil {
		return err
	}
	urls := localURLs()
	installer, err := update.New(update.Config{
		Downloader: localDownloader{files: map[string]string{urls.artifact: c.Artifact, urls.manifest: c.Manifest}, signature: signature, signatureURL: urls.signature},
		FS:         update.OSFileSystem{}, Space: statSpace{}, Clock: systemClock{}, Trust: policy,
		Device:     release.Device{Architecture: "linux-arm64", Protocol: protocol.ProtocolVersion},
		TargetPath: c.AgentPath, MetadataPath: c.MetadataPath, GatewayAuthority: localAuthority,
		MaxArtifactSize: c.MaxSize, DirectorySync: true,
	})
	if err != nil {
		return fmt.Errorf("configure local installer: %w", err)
	}
	offer := protocol.UpdateOffer{DeploymentID: "adb-recovery", Version: manifest.Version, BuildID: manifest.BuildID, ArtifactURL: urls.artifact, ManifestURL: urls.manifest, SignatureURL: urls.signature, Size: manifest.Size, SHA256: manifest.SHA256}
	result, err := installer.Install(context.Background(), offer)
	if err != nil {
		return fmt.Errorf("install local release: %w", err)
	}
	lines := []string{fmt.Sprintf("installed: version %s build %s", result.Metadata.Version, result.Metadata.BuildID)}
	for _, note := range policy.StatusNotes() {
		lines = append(lines, "warning: "+note)
	}
	return writeReport(w, lines)
}

func updateStatus(w io.Writer, c updateStatusCommand) error {
	raw, err := os.ReadFile(c.MetadataPath)
	if err != nil {
		return fmt.Errorf("read installed-release metadata: %w", err)
	}
	metadata, err := update.ParseMetadata(raw)
	if err != nil {
		return fmt.Errorf("parse installed-release metadata: %w", err)
	}
	return writeReport(w, []string{
		"version: " + metadata.Version,
		"build_id: " + metadata.BuildID,
		"architecture: " + metadata.Architecture,
		fmt.Sprintf("size: %d", metadata.Size),
		"sha256: " + metadata.SHA256,
		"installed_at: " + metadata.InstalledAt.Format(time.RFC3339),
	})
}

type localURLSet struct{ artifact, manifest, signature string }

func localURLs() localURLSet {
	return localURLSet{"https://" + localAuthority + "/artifact", "https://" + localAuthority + "/manifest", "https://" + localAuthority + "/signature"}
}

type localDownloader struct {
	files        map[string]string
	signature    []byte
	signatureURL string
}

func (d localDownloader) Fetch(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("local bundle context: %w", err)
	}
	if rawURL == d.signatureURL {
		return io.NopCloser(bytes.NewReader(d.signature)), nil
	}
	path, ok := d.files[rawURL]
	if !ok {
		return nil, errors.New("unknown local bundle resource")
	}
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open local bundle resource: %w", err)
	}
	return f, nil
}

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }
