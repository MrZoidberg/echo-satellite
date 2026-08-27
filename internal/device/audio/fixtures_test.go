package audio

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFixturesHaveExpectedFormats(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"tone_1k_16k_mono.wav", "sweep_16k_mono.wav", "silence_16k_mono.wav", "noise_16k_mono.wav"} {
		contents, err := os.ReadFile(filepath.Join(audioFixtureDir(), name)) //nolint:gosec // G304: fixed fixture names
		require.NoError(t, err)
		format, samples, err := ReadWAV(bytes.NewReader(contents))
		require.NoError(t, err)
		assert.Equal(t, Format{SampleRate: 16_000, Channels: 1, Layout: LayoutS16LE}, format)
		assert.Len(t, samples, 32_000)
	}

	raw, err := os.ReadFile(filepath.Join(audioFixtureDir(), "dot_mic_9ch_s24le.raw"))
	require.NoError(t, err)
	assert.Len(t, raw, 16_000*9*2*3)

	// This is a real two-second Echo Dot room capture, converted from the
	// diagnostic's canonical S16 WAV back to S24_3LE by shifting each retained
	// sample left eight bits. Unlike the synthetic fixtures, it is deliberately
	// exempt from TestFixtures_Regenerate's generator drift guard.
	roomRaw, err := os.ReadFile(filepath.Join(audioFixtureDir(), "dot_mic_9ch_s24le_room.raw"))
	require.NoError(t, err)
	assert.Len(t, roomRaw, 16_000*9*2*3)
	wantDigest, err := hex.DecodeString("4bce2e0954210a69daaad4811d15e1257ef75897ea6468887af8dffcea417841")
	require.NoError(t, err)
	digest := sha256.Sum256(roomRaw)
	assert.Equal(t, wantDigest, digest[:])
}
