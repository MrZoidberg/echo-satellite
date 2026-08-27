package main

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/release"
)

func fixture(t *testing.T, dir, file string) string {
	t.Helper()
	return filepath.Join("..", "..", "testdata", "updates", dir, file)
}

func TestParseArgs_Commands(t *testing.T) {
	t.Run("version", func(t *testing.T) {
		_, command, err := parseArgs([]string{"version"})
		require.NoError(t, err)
		assert.Equal(t, "version", command)
	})

	t.Run("release verify", func(t *testing.T) {
		o, command, err := parseArgs([]string{
			"release", "verify",
			"--manifest=m.json", "--artifact=echod", "--sig=m.sig", "--pubkey=m.pub",
		})
		require.NoError(t, err)
		assert.Equal(t, "release verify", command)
		assert.Equal(t, "m.json", o.Release.Verify.Manifest)
		assert.Equal(t, "echod", o.Release.Verify.Artifact)
		assert.Equal(t, "m.sig", o.Release.Verify.Sig)
		assert.Equal(t, "m.pub", o.Release.Verify.PubKey)
	})

	t.Run("manifest is required", func(t *testing.T) {
		_, _, err := parseArgs([]string{"release", "verify"})
		require.Error(t, err)
		assert.False(t, isHelpRequest(err))
	})

	t.Run("mic record", func(t *testing.T) {
		o, command, err := parseArgs([]string{"mic", "record", "--out=mic.wav", "--channels=all", "--seconds=2"})
		require.NoError(t, err)
		assert.Equal(t, "mic record", command)
		assert.Equal(t, "mic.wav", o.Mic.Record.Out)
		assert.Equal(t, "all", o.Mic.Record.Channels)
		assert.InDelta(t, 2.0, o.Mic.Record.Seconds, 0.001)
	})

	t.Run("speaker test", func(t *testing.T) {
		o, command, err := parseArgs([]string{"speaker", "test", "--to-file=spk.wav", "--resampler=linear"})
		require.NoError(t, err)
		assert.Equal(t, "speaker test", command)
		assert.Equal(t, "spk.wav", o.Speaker.Test.ToFile)
		assert.Equal(t, "linear", o.Speaker.Test.Resampler)
	})

	t.Run("led test", func(t *testing.T) {
		o, command, err := parseArgs([]string{"led", "test", "--root=/tmp/led", "--state=muted", "--seconds=2", "--current=42", "--clear"})
		require.NoError(t, err)
		assert.Equal(t, "led test", command)
		assert.Equal(t, "/tmp/led", o.LED.Test.Root)
		assert.Equal(t, "muted", o.LED.Test.State)
		assert.InDelta(t, 2.0, o.LED.Test.Seconds, 0.001)
		assert.Equal(t, uint8(42), o.LED.Test.Current)
		assert.True(t, o.LED.Test.Clear)
	})

	t.Run("buttons test", func(t *testing.T) {
		o, command, err := parseArgs([]string{"buttons", "test", "--input-dir=/tmp/input", "--sys-class-dir=/tmp/sys", "--from-file=events.bin", "--seconds=2"})
		require.NoError(t, err)
		assert.Equal(t, "buttons test", command)
		assert.Equal(t, "/tmp/input", o.Buttons.Test.InputDir)
		assert.Equal(t, "/tmp/sys", o.Buttons.Test.SysClassDir)
		assert.Equal(t, "events.bin", o.Buttons.Test.FromFile)
		assert.InDelta(t, 2.0, o.Buttons.Test.Seconds, 0.001)
	})

	t.Run("wake install", func(t *testing.T) {
		o, command, err := parseArgs([]string{"wake", "install", "okay_nabu", "--from=model.tflite", "--metadata=model.json", "--sha256=abcd", "--model-dir=/tmp/models", "--overwrite"})
		require.NoError(t, err)
		assert.Equal(t, "wake install", command)
		assert.Equal(t, "okay_nabu", o.Wake.Install.Args.ID)
		assert.Equal(t, "model.tflite", o.Wake.Install.From)
		assert.Equal(t, "/tmp/models", o.Wake.Install.ModelDir)
		assert.True(t, o.Wake.Install.Overwrite)
	})

	t.Run("wake list", func(t *testing.T) {
		o, command, err := parseArgs([]string{"wake", "list", "--model-dir=/tmp/models"})
		require.NoError(t, err)
		assert.Equal(t, "wake list", command)
		assert.Equal(t, "/tmp/models", o.Wake.List.ModelDir)
	})

	t.Run("wake diagnostics", func(t *testing.T) {
		o, command, err := parseArgs([]string{"wake", "test", "--from-file=silence.wav", "--model=hey", "--threshold=.8", "--vad-threshold=.4", "--vad-lookback-ms=160", "--no-vad", "--preroll-ms=320", "--save-preroll=/tmp/preroll"})
		require.NoError(t, err)
		assert.Equal(t, "wake test", command)
		assert.Equal(t, "hey", o.Wake.Test.Model)
		assert.True(t, o.Wake.Test.NoVAD)
		assert.Equal(t, 320, o.Wake.Test.PreRollMS)
		assert.Equal(t, 160, o.Wake.Test.VADLookbackMS)

		o, command, err = parseArgs([]string{"wake", "vad-test", "--from-file=silence.wav", "--print-steps"})
		require.NoError(t, err)
		assert.Equal(t, "wake vad-test", command)
		assert.True(t, o.Wake.VADTest.PrintSteps)

		o, command, err = parseArgs([]string{"wake", "test", "--start-audio=/tmp/start.wav", "--led-root=/tmp/led", "--green-on-wake"})
		require.NoError(t, err)
		assert.Equal(t, "wake test", command)
		assert.Equal(t, "/tmp/start.wav", o.Wake.Test.StartAudio)
		assert.Equal(t, "/tmp/led", o.Wake.Test.LEDRoot)
		assert.True(t, o.Wake.Test.GreenOnWake)
		assert.InDelta(t, 0.5, o.Wake.Test.Threshold, 0.0001)
		assert.InDelta(t, 0.5, o.Wake.Test.VADThreshold, 0.0001)
		assert.Equal(t, 1200, o.Wake.Test.VADLookbackMS)
		assert.Equal(t, 600, o.Wake.Test.PreRollMS)
	})

	t.Run("status JSON", func(t *testing.T) {
		o, command, err := parseArgs([]string{"status", "--json", "--root=/tmp/root", "--model-dir=/tmp/models"})
		require.NoError(t, err)
		assert.Equal(t, "status", command)
		assert.True(t, o.Status.JSON)
	})

	t.Run("wake install requires id and source", func(t *testing.T) {
		_, _, err := parseArgs([]string{"wake", "install"})
		require.Error(t, err)
	})

	t.Run("mic record requires out", func(t *testing.T) {
		_, _, err := parseArgs([]string{"mic", "record"})
		require.Error(t, err)
		assert.False(t, isHelpRequest(err))
	})

	t.Run("unknown command", func(t *testing.T) {
		_, _, err := parseArgs([]string{"wake", "score"})
		require.Error(t, err)
	})
}

func TestVerifyRelease_ValidBundle(t *testing.T) {
	var out bytes.Buffer
	err := verifyRelease(&out, verifyCommand{
		Manifest: fixture(t, "valid", "manifest.json"),
		Artifact: fixture(t, "valid", "echod"),
		Sig:      fixture(t, "valid", "manifest.sig"),
		PubKey:   fixture(t, "valid", "manifest.pub"),
	})

	require.NoError(t, err)
	assert.Contains(t, out.String(), "version 0.3.0")
	assert.Contains(t, out.String(), "sha256 matches")
	assert.Contains(t, out.String(), "release bundle accepted")
}

func TestVerifyRelease_TamperedArtifact(t *testing.T) {
	var out bytes.Buffer
	err := verifyRelease(&out, verifyCommand{
		Manifest: fixture(t, "tampered", "manifest.json"),
		Artifact: fixture(t, "tampered", "echod"),
		Sig:      fixture(t, "tampered", "manifest.sig"),
		PubKey:   fixture(t, "tampered", "manifest.pub"),
	})

	require.Error(t, err)
	require.ErrorIs(t, err, release.ErrDigestMismatch)
	assert.NotContains(t, out.String(), "accepted")
}

func TestVerifyRelease_WrongSignature(t *testing.T) {
	var out bytes.Buffer
	err := verifyRelease(&out, verifyCommand{
		Manifest: fixture(t, "badsig", "manifest.json"),
		Sig:      fixture(t, "badsig", "manifest.sig"),
		PubKey:   fixture(t, "badsig", "manifest.pub"),
	})

	assert.ErrorIs(t, err, release.ErrSignatureMismatch)
}

func TestVerifyRelease_UnsignedNeedsEscapeHatch(t *testing.T) {
	cmd := verifyCommand{
		Manifest: fixture(t, "unsigned", "manifest.json"),
		Artifact: fixture(t, "unsigned", "echod"),
		PubKey:   fixture(t, "unsigned", "manifest.pub"),
	}

	var out bytes.Buffer
	require.ErrorIs(t, verifyRelease(&out, cmd), release.ErrUnsignedRelease)

	out.Reset()
	cmd.AllowUnsigned = true
	require.NoError(t, verifyRelease(&out, cmd))
	assert.Contains(t, out.String(), "warning:")
	assert.Contains(t, out.String(), "release bundle accepted")
}

func TestVerifyRelease_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer

	err := verifyRelease(&out, verifyCommand{Manifest: filepath.Join(dir, "absent.json")})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read manifest")

	err = verifyRelease(&out, verifyCommand{
		Manifest: fixture(t, "valid", "manifest.json"),
		Sig:      filepath.Join(dir, "absent.sig"),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read signature")
}

func TestDispatch(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, dispatch(&out, "version", opts{}))
	assert.Contains(t, out.String(), "version:")

	assert.Error(t, dispatch(&out, "", opts{}), "no command means nothing to do")
}
