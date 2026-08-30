package discovery

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/MrZoidberg/echo-satellite/internal/protocol"
)

func TestPairingStoreSaveLoadAndAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state", "paired-gateway.json")
	store := PairingStore{Path: path}

	got, err := store.Load()
	require.ErrorIs(t, err, ErrNoPairing)
	assert.Nil(t, got)

	want := gateway("home-gateway", "gateway.local.")
	require.NoError(t, store.Save(want))
	got, err = store.Load()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)
	assert.NoFileExists(t, path+".part")

	// An interrupted staged write never replaces the authenticated pairing.
	require.NoError(t, os.WriteFile(path+".part", []byte(`{"server_id":"other"}`), 0o600))
	got, err = store.Load()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, *got)

	data, err := os.ReadFile(path) //nolint:gosec // G304: test controls the temporary path.
	require.NoError(t, err)
	assert.NotContains(t, string(data), "token")
	assert.NotContains(t, string(data), "credential")
}

func TestPairingStorePreservesCorruptState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paired-gateway.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0o600))

	got, err := (PairingStore{Path: path}).Load()
	require.ErrorIs(t, err, ErrCorruptPairing)
	assert.Nil(t, got)
	data, readErr := os.ReadFile(path) //nolint:gosec // G304: test controls the temporary path.
	require.NoError(t, readErr)
	assert.Equal(t, []byte("not json"), data)
}

func TestPairingStoreRejectsUnexpectedCredentialField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "paired-gateway.json")
	valid, err := marshalPairing(gateway("home-gateway", "gateway.local."))
	require.NoError(t, err)
	credentialBearing := append([]byte(nil), valid[:len(valid)-1]...)
	credentialBearing = append(credentialBearing, []byte(`,"token":"must-not-persist"}`)...)
	require.NoError(t, os.WriteFile(path, credentialBearing, 0o600))

	got, err := (PairingStore{Path: path}).Load()
	require.ErrorIs(t, err, ErrCorruptPairing)
	assert.Nil(t, got)
	data, readErr := os.ReadFile(path) //nolint:gosec // G304: test controls the temporary path.
	require.NoError(t, readErr)
	assert.Equal(t, credentialBearing, data)
}

func TestPairingStoreRejectsInvalidInstance(t *testing.T) {
	bad := gateway("home-gateway", "gateway.local.")
	bad.TXT.Protocol = protocol.ProtocolVersion + 1
	bad.TXT.ServerID = "other-gateway"
	err := (PairingStore{Path: filepath.Join(t.TempDir(), "pairing.json")}).Save(bad)
	require.Error(t, err)
}
