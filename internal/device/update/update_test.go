package update

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
	"github.com/MrZoidberg/echo-satellite/internal/release"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstaller_Install(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, OSFileSystem{})
	result, err := fixture.installer.Install(context.Background(), fixture.offer)
	require.NoError(t, err)
	assert.True(t, result.Committed)
	actual, err := os.ReadFile(fixture.target)
	require.NoError(t, err)
	assert.Equal(t, fixture.artifact, actual)
	info, err := os.Stat(fixture.target)
	require.NoError(t, err)
	assert.Equal(t, fs.FileMode(0o755), info.Mode().Perm())
	raw, err := os.ReadFile(fixture.metadata)
	require.NoError(t, err)
	metadata, err := ParseMetadata(raw)
	require.NoError(t, err)
	assert.Equal(t, fixture.manifest.BuildID, metadata.BuildID)
	assert.True(t, MetadataMatchesRevision(metadata, fixture.manifest.BuildID))
	assert.False(t, MetadataMatchesRevision(metadata, "different"))
}

func TestInstaller_PreCommitFailuresPreserveExistingAgent(t *testing.T) {
	t.Parallel()
	for name, makeFS := range map[string]func() FileSystem{
		"create": func() FileSystem { return &faultFS{create: errors.New("create")} },
		"write":  func() FileSystem { return &faultFS{writeAt: 1, write: errors.New("write")} },
		"chmod":  func() FileSystem { return &faultFS{chmod: errors.New("chmod")} },
		"fsync":  func() FileSystem { return &faultFS{sync: errors.New("sync")} },
		"rename": func() FileSystem { return &faultFS{rename: errors.New("rename")} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newFixture(t, makeFS())
			result, err := fixture.installer.Install(context.Background(), fixture.offer)
			require.Error(t, err)
			assert.False(t, result.Committed)
			require.NotErrorIs(t, err, ErrCommitted)
			actual, readErr := os.ReadFile(fixture.target)
			require.NoError(t, readErr)
			assert.Equal(t, []byte("old agent"), actual)
		})
	}
}

func TestInstaller_TrustFailurePreservesExistingAgent(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, OSFileSystem{})
	fixture.installer.config.Trust = rejectTrust{}
	_, err := fixture.installer.Install(context.Background(), fixture.offer)
	require.Error(t, err)
	assertAgentUntouched(t, fixture.target)
}

func TestInstaller_PostCommitFailuresAreReportedAsCommitted(t *testing.T) {
	t.Parallel()
	for name, makeFS := range map[string]func() FileSystem{
		"agent directory fsync": func() FileSystem { return &faultFS{syncDir: errors.New("directory sync")} },
		"metadata write":        func() FileSystem { return &faultFS{writeAt: 2, write: errors.New("metadata write")} },
		"metadata rename":       func() FileSystem { return &faultFS{renameAt: 2, rename: errors.New("metadata rename")} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fixture := newFixture(t, makeFS())
			result, err := fixture.installer.Install(context.Background(), fixture.offer)
			require.ErrorIs(t, err, ErrCommitted)
			assert.True(t, result.Committed)
			actual, readErr := os.ReadFile(fixture.target)
			require.NoError(t, readErr)
			assert.Equal(t, fixture.artifact, actual)
		})
	}
}

func TestInstaller_RejectsOversizedAndBusyOffersWithoutWriting(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, OSFileSystem{})
	fixture.offer.Size++
	_, err := fixture.installer.Install(context.Background(), fixture.offer)
	require.ErrorIs(t, err, ErrOfferMismatch)
	assertAgentUntouched(t, fixture.target)

	busy := newFixture(t, OSFileSystem{})
	busy.installer.config.Voice = activeVoice(true)
	_, err = busy.installer.Install(context.Background(), busy.offer)
	require.ErrorIs(t, err, ErrBusy)
	assertAgentUntouched(t, busy.target)
}

func TestInstaller_RejectsExtraArtifactDataAndCancellation(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, OSFileSystem{})
	fixture.downloader.bodies[fixture.offer.ArtifactURL] = append(fixture.artifact, 'x')
	_, err := fixture.installer.Install(context.Background(), fixture.offer)
	require.ErrorIs(t, err, ErrTooLarge)
	assertAgentUntouched(t, fixture.target)

	canceled := newFixture(t, OSFileSystem{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = canceled.installer.Install(ctx, canceled.offer)
	require.ErrorIs(t, err, context.Canceled)
	assertAgentUntouched(t, canceled.target)
}

func TestInstaller_RequiresFreeSpaceMarginAndArchitecture(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, OSFileSystem{})
	required, err := stagingSpace(fixture.offer.Size)
	require.NoError(t, err)
	fixture.installer.config.Space = fixedSpace(required - 1)
	_, err = fixture.installer.Install(context.Background(), fixture.offer)
	require.ErrorIs(t, err, ErrInsufficientSpace)
	assertAgentUntouched(t, fixture.target)

	config := fixture.installer.config
	config.Device.Architecture = ""
	_, err = New(config)
	require.Error(t, err)
}

func TestInstaller_ReconcileMetadata(t *testing.T) {
	t.Parallel()
	fixture := newFixture(t, OSFileSystem{})
	_, err := fixture.installer.Install(context.Background(), fixture.offer)
	require.NoError(t, err)
	metadata, matches, err := fixture.installer.ReconcileMetadata(fixture.manifest.BuildID)
	require.NoError(t, err)
	assert.True(t, matches)
	assert.Equal(t, fixture.manifest.BuildID, metadata.BuildID)
	_, matches, err = fixture.installer.ReconcileMetadata("other-build")
	require.NoError(t, err)
	assert.False(t, matches)
	require.NoError(t, os.WriteFile(fixture.metadata, []byte(`null`), 0o600))
	_, _, err = fixture.installer.ReconcileMetadata(fixture.manifest.BuildID)
	require.Error(t, err)
}

func TestParseMetadata_RejectsTrailingDocument(t *testing.T) {
	t.Parallel()
	_, err := ParseMetadata([]byte(`{"schema":1,"version":"v","build_id":"b","architecture":"linux-arm64","size":1,"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","installed_at":"2026-01-01T00:00:00Z"} null`))
	require.Error(t, err)
}

func TestValidateGatewayURL(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"http://gateway.example/artifact",
		"https://user@gateway.example/artifact",
		"https://other.example/artifact",
	} {
		require.Error(t, validateGatewayURL(rawURL, "gateway.example"))
	}
	assert.NoError(t, validateGatewayURL("https://gateway.example/artifact?token=secret", "gateway.example"))
}

func TestHTTPDownloader_SanitizesCredentialBearingURLFailures(t *testing.T) {
	t.Parallel()
	downloader := HTTPDownloader{Client: &http.Client{Transport: failingTransport{}}, GatewayAuthority: "gateway.example"}
	_, err := downloader.Fetch(context.Background(), "https://gateway.example/artifact?token=secret")
	require.ErrorIs(t, err, ErrDownloadFailed)
	assert.NotContains(t, err.Error(), "secret")
}

type fixture struct {
	installer  *Installer
	downloader *memoryDownloader
	offer      protocol.UpdateOffer
	manifest   release.Manifest
	artifact   []byte
	target     string
	metadata   string
}

func newFixture(t *testing.T, filesystem FileSystem) fixture {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", "echod")
	metadata := filepath.Join(dir, "etc", "installed-release.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Dir(metadata), 0o750))
	require.NoError(t, os.WriteFile(target, []byte("old agent"), 0o600))
	artifact := []byte("new signed agent")
	digest := sha256.Sum256(artifact)
	manifest := release.Manifest{Schema: 1, Version: "0.2.0", BuildID: "build-new", Architecture: "linux-arm64", Size: int64(len(artifact)), SHA256: hex.EncodeToString(digest[:]), ProtocolMin: 1, ProtocolMax: 1, ReleasedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	// ParseManifest requires JSON rather than canonical signing bytes' domain.
	manifestRaw := []byte(`{"schema":1,"version":"0.2.0","build_id":"build-new","architecture":"linux-arm64","size":16,"sha256":"` + manifest.SHA256 + `","protocol_min":1,"protocol_max":1,"released_at":"2026-01-01T00:00:00Z"}`)
	offer := protocol.UpdateOffer{DeploymentID: "deploy-1", Version: manifest.Version, BuildID: manifest.BuildID, ArtifactURL: "https://gateway.example/artifact?token=secret", ManifestURL: "https://gateway.example/manifest?token=secret", SignatureURL: "https://gateway.example/signature?token=secret", Size: manifest.Size, SHA256: manifest.SHA256}
	downloader := &memoryDownloader{bodies: map[string][]byte{offer.ArtifactURL: artifact, offer.ManifestURL: manifestRaw, offer.SignatureURL: []byte("signature")}}
	installer, err := New(Config{Downloader: downloader, FS: filesystem, Space: fixedSpace(1 << 30), Clock: fixedClock{}, Trust: allowTrust{}, Device: release.Device{Architecture: "linux-arm64", Protocol: 1}, TargetPath: target, MetadataPath: metadata, GatewayAuthority: "gateway.example", MaxArtifactSize: 1 << 20, DirectorySync: true})
	require.NoError(t, err)
	return fixture{installer: installer, downloader: downloader, offer: offer, manifest: manifest, artifact: artifact, target: target, metadata: metadata}
}

func assertAgentUntouched(t *testing.T, path string) {
	t.Helper()
	actual, err := os.ReadFile(path) //nolint:gosec // test passes a path created below t.TempDir.
	require.NoError(t, err)
	assert.Equal(t, []byte("old agent"), actual)
}

type memoryDownloader struct{ bodies map[string][]byte }

func (d *memoryDownloader) Fetch(_ context.Context, rawURL string) (io.ReadCloser, error) {
	raw, ok := d.bodies[rawURL]
	if !ok {
		return nil, errors.New("unexpected URL")
	}
	return io.NopCloser(bytes.NewReader(raw)), nil
}

type fixedSpace int64

func (s fixedSpace) Available(string) (int64, error) { return int64(s), nil }

type fixedClock struct{}

func (fixedClock) Now() time.Time { return time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC) }

type allowTrust struct{}

func (allowTrust) Check(release.Manifest, []byte) error { return nil }

type rejectTrust struct{}

func (rejectTrust) Check(release.Manifest, []byte) error { return errors.New("signature rejected") }

type activeVoice bool

func (v activeVoice) Active() bool { return bool(v) }

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("network unavailable")
}

type faultFS struct {
	create, chmod, sync, rename, syncDir, write error
	renameAt, renames, writeAt, writes          int
}

func (f *faultFS) CreateTemp(dir, pattern string, perm fs.FileMode) (File, string, error) {
	if f.create != nil {
		return nil, "", f.create
	}
	file, path, err := OSFileSystem{}.CreateTemp(dir, pattern, perm)
	if err != nil {
		return nil, "", err
	}
	return &faultFile{File: file, owner: f, failSync: f.sync}, path, nil
}
func (f *faultFS) Open(path string) (File, error) {
	file, err := OSFileSystem{}.Open(path)
	if err != nil {
		return nil, err
	}
	return &faultFile{File: file, failSync: f.sync}, nil
}
func (f *faultFS) ReadFile(path string) ([]byte, error) { return OSFileSystem{}.ReadFile(path) }
func (f *faultFS) Rename(oldpath, newpath string) error {
	f.renames++
	if f.rename != nil && (f.renameAt == 0 || f.renames == f.renameAt) {
		return f.rename
	}
	return OSFileSystem{}.Rename(oldpath, newpath)
}
func (f *faultFS) Remove(path string) error { return OSFileSystem{}.Remove(path) }
func (f *faultFS) Chmod(path string, perm fs.FileMode) error {
	if f.chmod != nil {
		return f.chmod
	}
	return OSFileSystem{}.Chmod(path, perm)
}
func (f *faultFS) SyncDir(path string) error {
	if f.syncDir != nil {
		return f.syncDir
	}
	return OSFileSystem{}.SyncDir(path)
}

type faultFile struct {
	File
	owner    *faultFS
	failSync error
}

func (f *faultFile) Sync() error {
	if f.failSync != nil {
		return f.failSync
	}
	if err := f.File.Sync(); err != nil {
		return fmt.Errorf("sync test file: %w", err)
	}
	return nil
}
func (f *faultFile) Write(raw []byte) (int, error) {
	f.owner.writes++
	if f.owner.write != nil && f.owner.writes == f.owner.writeAt {
		return 0, f.owner.write
	}
	n, err := f.File.Write(raw)
	if err != nil {
		return n, fmt.Errorf("write test file: %w", err)
	}
	return n, nil
}
