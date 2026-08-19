package release

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func dot() Device {
	return Device{Architecture: "linux-arm64", Protocol: protocol.ProtocolVersion, SupervisorVersion: 1}
}

func TestEligible_Valid(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "valid", "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, Eligible(m, dot()))
}

func TestEligible_SupervisorTooOld(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "supervisor-too-new", "manifest.json"))
	require.NoError(t, err)

	err = Eligible(m, dot())

	require.Error(t, err)
	require.ErrorIs(t, err, ErrSupervisorTooOld)
	assert.Contains(t, err.Error(), "99")
}

func TestEligible_ArchitectureMismatch(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "valid", "manifest.json"))
	require.NoError(t, err)

	dev := dot()
	dev.Architecture = "linux-amd64"

	assert.ErrorIs(t, Eligible(m, dev), ErrArchitectureMismatch)
}

func TestEligible_ProtocolRange(t *testing.T) {
	m := manifestFor(fixtureArtifact, "0.5.0")
	m.ProtocolMin, m.ProtocolMax = 2, 3

	t.Run("device too old", func(t *testing.T) {
		dev := dot()
		dev.Protocol = 1
		assert.ErrorIs(t, Eligible(m, dev), ErrProtocolIncompatible)
	})

	t.Run("device too new", func(t *testing.T) {
		dev := dot()
		dev.Protocol = 4
		assert.ErrorIs(t, Eligible(m, dev), ErrProtocolIncompatible)
	})

	t.Run("inside range", func(t *testing.T) {
		dev := dot()
		dev.Protocol = 3
		require.NoError(t, Eligible(m, dev))
	})
}

func TestEligible_InvalidManifest(t *testing.T) {
	broken := manifestFor(fixtureArtifact, "0.3.0")
	broken.SHA256 = "nope"

	assert.ErrorIs(t, Eligible(broken, dot()), ErrInvalidManifest)
}

func TestEligible_UnknownDeviceArchitectureIsNotChecked(t *testing.T) {
	m, err := ParseManifest(readFixture(t, "valid", "manifest.json"))
	require.NoError(t, err)

	dev := dot()
	dev.Architecture = ""

	require.NoError(t, Eligible(m, dev), "a device that did not report an architecture is not blocked on it")
}
