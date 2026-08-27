package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/device/audio"
)

func audioFixture(name string) string { return filepath.Join("..", "..", "testdata", "audio", name) }

func TestMicRecord_WritesWAVFromFixtureSource(t *testing.T) {
	out := filepath.Join(t.TempDir(), "mic.wav")
	var report bytes.Buffer
	require.NoError(t, micRecord(&report, micRecordCommand{Seconds: 0.02, Out: out, Channels: "all", FromFile: audioFixture("dot_mic_9ch_s24le.raw")}))
	file, err := os.Open(out) //nolint:gosec // Test opens the path created in its private temporary directory.
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	format, samples, err := audio.ReadWAV(file)
	require.NoError(t, err)
	assert.Equal(t, 9, format.Channels)
	assert.Len(t, samples, 320*9)
}

func TestMicRecord_PrintsPerChannelLevels(t *testing.T) {
	var report bytes.Buffer
	err := micRecord(&report, micRecordCommand{Seconds: 0.02, Out: filepath.Join(t.TempDir(), "mic.wav"), Channels: "all", FromFile: audioFixture("dot_mic_9ch_s24le.raw"), PrintLevels: true})
	require.NoError(t, err)
	expectedPeaks := []string{"-24.29", "-20.77", "-18.27", "-16.33", "-14.75", "-13.41", "-12.25", "-11.22", "-10.31"}
	for channel, peak := range expectedPeaks {
		assert.Contains(t, report.String(), "channel mic"+string(rune('0'+channel))+": peak "+peak+" dBFS")
	}
}

func TestParseMicChannels_RejectsLoopbackAndDuplicates(t *testing.T) {
	_, err := parseMicChannels("mic7", 9)
	require.Error(t, err)
	_, err = parseMicChannels("mic0,mic0", 9)
	require.Error(t, err)
}
