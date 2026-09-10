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
	"strings"
	"testing"

	"github.com/MrZoidberg/echo-satellite/internal/release"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootstrapWithRunner_FreshInstall(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "launcher.sh")
	agent := filepath.Join(dir, "echod")
	require.NoError(t, os.WriteFile(launcher, []byte("#!/system/bin/sh\n"+launcherMarker+"\n"), 0o600))
	require.NoError(t, os.WriteFile(agent, []byte("agent"), 0o600))
	runner := &recordingRunner{}

	var out bytes.Buffer
	err := bootstrapWithRunner(context.Background(), &out, updateBootstrapCommand{ADB: "adb", Serial: "dot", Launcher: launcher, Agent: agent}, runner)
	require.NoError(t, err)
	require.Len(t, runner.calls, 5)
	assert.Equal(t, []string{"-s", "dot", "push", launcher, "/data/local/tmp/echo-satellite-launcher.part"}, runner.calls[0].args)
	assert.Equal(t, []string{"-s", "dot", "push", agent, "/data/local/tmp/echod.part"}, runner.calls[1].args)
	assert.Equal(t, []string{"-s", "dot", "shell", "chmod", "0700", "/data/local/tmp/echo-satellite-bootstrap.sh"}, runner.calls[3].args)
	assert.Equal(t, []string{"-s", "dot", "shell", "su", "-c", "/data/local/tmp/echo-satellite-bootstrap.sh"}, runner.calls[4].args)
	assert.Contains(t, out.String(), "bootstrap installed")
}

func TestBootstrapWithRunner_InterruptionAndConflictLeaveRemoteInstallUnchanged(t *testing.T) {
	dir := t.TempDir()
	launcher := filepath.Join(dir, "launcher.sh")
	agent := filepath.Join(dir, "echod")
	require.NoError(t, os.WriteFile(launcher, []byte(launcherMarker), 0o600))
	require.NoError(t, os.WriteFile(agent, []byte("agent"), 0o600))

	for name, failCall := range map[string]int{"push interruption": 1, "conflicting launcher": 5} {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{failCall: failCall}
			err := bootstrapWithRunner(context.Background(), io.Discard, updateBootstrapCommand{ADB: "adb", Serial: "dot", Launcher: launcher, Agent: agent}, runner)
			require.Error(t, err)
			if failCall == 1 {
				assert.Len(t, runner.calls, 1, "agent is never pushed after launcher push fails")
			} else {
				require.Len(t, runner.calls, 5)
				assert.Equal(t, "/data/local/tmp/echo-satellite-bootstrap.sh", runner.calls[4].args[len(runner.calls[4].args)-1])
			}
		})
	}
}

func TestBootstrapScript_IdempotenceAndBackupRules(t *testing.T) {
	script := bootstrapScript("/tmp/launcher", "/tmp/agent")
	assert.Contains(t, script, "cmp -s \"/tmp/launcher\" \"$launcher\"")
	assert.Contains(t, script, "cp \"$launcher\" \"$backup.echo-satellite-new\"")
	assert.Contains(t, script, "mv \"$launcher.echo-satellite-new\" \"$launcher\"")
	assert.Contains(t, script, "existing launcher backup would be overwritten")
	assert.Contains(t, script, "unrecognized conflicting launcher")
	assert.Contains(t, script, "unrecognized conflicting direct-start hook")
	assert.Contains(t, script, "mv \"$direct_start\" \"$direct_backup\"")
	assert.Less(t, strings.Index(script, "mv \"$direct_start\" \"$direct_backup\""), strings.LastIndex(script, "mv \"/tmp/agent\" /data/local/bin/echod"))
	assert.Less(t, strings.Index(script, "mv \"$launcher.echo-satellite-new\" \"$launcher\""), strings.LastIndex(script, "mv \"/tmp/agent\" /data/local/bin/echod"))
	assert.Less(t, strings.Index(script, "mkdir -p /data/local/etc/echo-satellite"), strings.LastIndex(script, "mv \"/tmp/agent\" /data/local/bin/echod"))
}

func TestBootstrapScriptWithComparator_UsesQualifiedBusyBox(t *testing.T) {
	script := bootstrapScriptWithComparator("/tmp/launcher", "/tmp/agent", "/data/adb/magisk/busybox cmp")
	assert.Contains(t, script, "/data/adb/magisk/busybox cmp -s \"/tmp/launcher\" \"$launcher\"")
	assert.Contains(t, script, "/data/adb/magisk/busybox cmp -s \"/tmp/agent\" /data/local/bin/echod")
}

func TestBootstrapScript_FakeADBFilesystemScenarios(t *testing.T) {
	t.Run("fresh install and idempotence", func(t *testing.T) {
		fixture := newBootstrapShellFixture(t)
		fixture.run(t)
		assertFileBytes(t, fixture.launcher, fixture.launcherPayload)
		assertFileBytes(t, fixture.agentTarget, fixture.agentPayload)
		fixture.stage(t)
		fixture.run(t)
		assertFileBytes(t, fixture.launcher, fixture.launcherPayload)
		assertFileBytes(t, fixture.agentTarget, fixture.agentPayload)
		_, err := os.Stat(fixture.launcher + ".echo-satellite-backup")
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("recognized hooks are backed up", func(t *testing.T) {
		fixture := newBootstrapShellFixture(t)
		oldLauncher := []byte("#!/system/bin/sh\n" + launcherMarker + "\nold launcher\n")
		oldDirect := []byte("#!/system/bin/sh\n" + launcherMarker + "\nold direct start\n")
		require.NoError(t, os.WriteFile(fixture.launcher, oldLauncher, 0o600))
		require.NoError(t, os.WriteFile(fixture.directStart, oldDirect, 0o600))
		fixture.run(t)
		assertFileBytes(t, fixture.launcher+".echo-satellite-backup", oldLauncher)
		assertFileBytes(t, fixture.directStart+".echo-satellite-backup", oldDirect)
		assertFileBytes(t, fixture.launcher, fixture.launcherPayload)
	})

	t.Run("interruption before staged agent leaves hooks untouched", func(t *testing.T) {
		fixture := newBootstrapShellFixture(t)
		oldLauncher := []byte("#!/system/bin/sh\n" + launcherMarker + "\nold launcher\n")
		require.NoError(t, os.WriteFile(fixture.launcher, oldLauncher, 0o600))
		require.NoError(t, os.Remove(fixture.stagedAgent))
		err := fixture.runError()
		require.Error(t, err)
		assertFileBytes(t, fixture.launcher, oldLauncher)
		_, statErr := os.Stat(fixture.launcher + ".echo-satellite-backup")
		assert.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("unrecognized conflict leaves agent and hook untouched", func(t *testing.T) {
		fixture := newBootstrapShellFixture(t)
		foreign := []byte("#!/system/bin/sh\nforeign hook\n")
		oldAgent := []byte("known good")
		require.NoError(t, os.WriteFile(fixture.launcher, foreign, 0o600))
		require.NoError(t, os.WriteFile(fixture.agentTarget, oldAgent, 0o600))
		err := fixture.runError()
		require.Error(t, err)
		assertFileBytes(t, fixture.launcher, foreign)
		assertFileBytes(t, fixture.agentTarget, oldAgent)
	})
}

func TestUpdateInstall_SignedDowngradeAndStatus(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", "echod")
	metadata := filepath.Join(dir, "etc", "installed-release.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	require.NoError(t, os.WriteFile(target, []byte("newer agent"), 0o600))

	// The fixture is a signed 0.3.0 release. The existing arbitrary bytes model
	// a newer installed build: no version comparison is used to block recovery.
	var out bytes.Buffer
	err := updateInstall(&out, updateInstallCommand{
		Artifact: fixture(t, "valid", "echod"), Manifest: fixture(t, "valid", "manifest.json"), Sig: fixture(t, "valid", "manifest.sig"), PubKey: fixture(t, "valid", "manifest.pub"),
		AgentPath: target, MetadataPath: metadata, MaxSize: 1 << 20,
	})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "version 0.3.0")
	actual, err := os.ReadFile(target) //nolint:gosec // G304: test target is created below t.TempDir.
	require.NoError(t, err)
	want, err := os.ReadFile(fixture(t, "valid", "echod"))
	require.NoError(t, err)
	assert.Equal(t, want, actual)

	out.Reset()
	require.NoError(t, updateStatus(&out, updateStatusCommand{MetadataPath: metadata}))
	assert.Contains(t, out.String(), "build_id: git-abc123")
}

func TestUpdateInstall_UnsignedRejectedUnlessExplicitlyAllowed(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "bin", "echod")
	metadata := filepath.Join(dir, "etc", "installed-release.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(target), 0o750))
	require.NoError(t, os.WriteFile(target, []byte("known good"), 0o600))
	command := updateInstallCommand{Artifact: fixture(t, "valid", "echod"), Manifest: fixture(t, "valid", "manifest.json"), AgentPath: target, MetadataPath: metadata, MaxSize: 1 << 20}
	err := updateInstall(io.Discard, command)
	require.ErrorIs(t, err, release.ErrUnsignedRelease)
	actual, readErr := os.ReadFile(target) //nolint:gosec // G304: test target is created below t.TempDir.
	require.NoError(t, readErr)
	assert.Equal(t, []byte("known good"), actual)

	command.AllowUnsigned = true
	var out bytes.Buffer
	require.NoError(t, updateInstall(&out, command))
	assert.Contains(t, out.String(), "warning: unsigned development releases")
}

type recordedCall struct {
	name string
	args []string
}

type recordingRunner struct {
	calls    []recordedCall
	failCall int
}

type bootstrapShellFixture struct {
	serviceDir, binDir, metadataDir    string
	launcher, directStart, agentTarget string
	stagedLauncher, stagedAgent        string
	launcherPayload, agentPayload      []byte
}

func newBootstrapShellFixture(t *testing.T) bootstrapShellFixture {
	t.Helper()
	root := t.TempDir()
	serviceDir := filepath.Join(root, "service.d")
	binDir := filepath.Join(root, "bin")
	require.NoError(t, os.MkdirAll(serviceDir, 0o700))
	require.NoError(t, os.MkdirAll(binDir, 0o700))
	launcherPayload := []byte("#!/system/bin/sh\n" + launcherMarker + "\nnew launcher\n")
	agentPayload := []byte("new agent")
	stagedLauncher := filepath.Join(root, "launcher.part")
	stagedAgent := filepath.Join(root, "agent.part")
	require.NoError(t, os.WriteFile(stagedLauncher, launcherPayload, 0o600))
	require.NoError(t, os.WriteFile(stagedAgent, agentPayload, 0o600))
	return bootstrapShellFixture{
		serviceDir: serviceDir, binDir: binDir, metadataDir: filepath.Join(root, "metadata"),
		launcher: filepath.Join(serviceDir, launcherName), directStart: filepath.Join(serviceDir, directStartName), agentTarget: filepath.Join(binDir, "echod"),
		stagedLauncher: stagedLauncher, stagedAgent: stagedAgent, launcherPayload: launcherPayload, agentPayload: agentPayload,
	}
}

func (f bootstrapShellFixture) stage(t *testing.T) {
	t.Helper()
	require.NoError(t, os.WriteFile(f.stagedLauncher, f.launcherPayload, 0o600))
	require.NoError(t, os.WriteFile(f.stagedAgent, f.agentPayload, 0o600))
}

func (f bootstrapShellFixture) run(t *testing.T) {
	t.Helper()
	require.NoError(t, f.runError())
}

func (f bootstrapShellFixture) runError() error {
	script := bootstrapScript(f.stagedLauncher, f.stagedAgent)
	script = strings.ReplaceAll(script, legacyServiceDir, f.serviceDir)
	script = strings.ReplaceAll(script, "/data/local/etc/echo-satellite", f.metadataDir)
	script = strings.ReplaceAll(script, "/data/local/bin", f.binDir)
	if err := exec.Command("sh", "-c", script).Run(); err != nil { //nolint:gosec // test runs only a locally generated fixed script.
		return fmt.Errorf("run fake ADB shell: %w", err)
	}
	return nil
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path) //nolint:gosec // G304: test path is created below t.TempDir.
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, recordedCall{name: name, args: args})
	if len(r.calls) == r.failCall {
		return []byte("failure"), errors.New("adb failed")
	}
	return nil, nil
}

func TestLauncherPayload_OnlyOwnsRestartPolicy(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "device_payloads", "launcher", launcherName))
	require.NoError(t, err)
	payload := string(raw)
	assert.Contains(t, payload, "CONTROLLED_RESTART=75")
	assert.Contains(t, payload, "1) delay=2")
	assert.Contains(t, payload, "16) delay=32")
	assert.Contains(t, payload, "*) delay=60")
	assert.Contains(t, payload, "runtime\" -ge 60")
	for _, forbidden := range []string{"json", "rollback", "manifest", "health"} {
		assert.NotContains(t, strings.ToLower(payload), forbidden)
	}
}
