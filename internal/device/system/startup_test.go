package system

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type startupServices struct {
	calls *[]string
	fail  string
}

func (s startupServices) Stop(name string) error {
	*s.calls = append(*s.calls, "stop "+name)
	if name == s.fail {
		return errors.New("failed")
	}
	return nil
}

type startupLED struct {
	calls    *[]string
	clearErr error
}

func (l startupLED) SetBootAnimation(bool) error { *l.calls = append(*l.calls, "boot"); return nil }
func (l startupLED) Clear() error                { *l.calls = append(*l.calls, "clear"); return l.clearErr }

func TestStartupPreparer_OrdersPreparationAndRestoresMute(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "gpio444"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gpio444", "value"), []byte("1\n"), 0o600))
	calls := []string{}
	p := StartupPreparer{Services: startupServices{calls: &calls}, LED: startupLED{calls: &calls}, GPIORoot: root, Animation: StartupAnimationFunc(func() error { calls = append(calls, "animate"); return nil })}
	cleanup, err := p.Prepare()
	require.NoError(t, err)
	assert.Equal(t, []string{"stop ledcontroller", "stop mdnsd", "boot", "clear", "animate"}, calls)
	require.NoError(t, os.WriteFile(filepath.Join(root, "gpio444", "value"), []byte("0\n"), 0o600))
	require.NoError(t, cleanup())
	value, err := os.ReadFile(filepath.Join(root, "gpio444", "value")) //nolint:gosec // Test reads a fixed path under its private temporary directory.
	require.NoError(t, err)
	assert.Equal(t, "1\n", string(value))
}

func TestStartupPreparer_FailureStopsLaterActionsAndRestoresMute(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "gpio444"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gpio444", "value"), []byte("0\n"), 0o600))
	calls := []string{}
	p := StartupPreparer{Services: startupServices{calls: &calls, fail: "mdnsd"}, LED: startupLED{calls: &calls}, GPIORoot: root, Animation: StartupAnimationFunc(func() error { calls = append(calls, "animate"); return nil })}
	_, err := p.Prepare()
	require.Error(t, err)
	assert.Equal(t, []string{"stop ledcontroller", "stop mdnsd", "clear"}, calls)
}

func TestStartupPreparer_ExportsAbsentMuteGPIO(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "export"), nil, 0o600))
	require.NoError(t, os.Mkdir(filepath.Join(root, "gpio444"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "gpio444", "direction"), nil, 0o600))
	calls := []string{}
	p := StartupPreparer{Services: startupServices{calls: &calls}, LED: startupLED{calls: &calls}, GPIORoot: root, Animation: StartupAnimationFunc(func() error { return nil })}
	cleanup, err := p.Prepare()
	require.NoError(t, err)
	require.NoError(t, cleanup())
}
