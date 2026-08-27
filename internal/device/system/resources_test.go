package system

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadUsage_ParsesRSSAndCPUTime(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "stat"), []byte("42 (echo daemon) S 1 2 3 4 5 6 7 8 9 10 150 50 0 0 0\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "status"), []byte("Name:\techod\nVmRSS:\t1234 kB\n"), 0o600))

	usage, err := ReadUsage(root)
	require.NoError(t, err)
	assert.Equal(t, 2*time.Second, usage.CPUTime)
	assert.Equal(t, uint64(1234*1024), usage.RSSBytes)
}

func TestSampler_ComputesCPUPercentBetweenSamples(t *testing.T) {
	t.Parallel()

	var sampler Sampler
	assert.InDelta(t, 0.0, sampler.CPUPercent(Usage{CPUTime: time.Second}, time.Unix(10, 0)), 0.0001)
	assert.InDelta(t, 50.0, sampler.CPUPercent(Usage{CPUTime: 2 * time.Second}, time.Unix(12, 0)), 0.0001)
}

func TestSampler_HandlesCounterAndClockRegression(t *testing.T) {
	t.Parallel()

	var sampler Sampler
	sampler.CPUPercent(Usage{CPUTime: 2 * time.Second}, time.Unix(10, 0))
	assert.InDelta(t, 0.0, sampler.CPUPercent(Usage{CPUTime: time.Second}, time.Unix(11, 0)), 0.0001)
	assert.InDelta(t, 0.0, sampler.CPUPercent(Usage{CPUTime: 3 * time.Second}, time.Unix(9, 0)), 0.0001)
}

func TestReadUsage_RejectsMalformedFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "stat"), []byte("malformed"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "status"), []byte("VmRSS:\t1 kB\n"), 0o600))
	_, err := ReadUsage(root)
	require.Error(t, err)
}
