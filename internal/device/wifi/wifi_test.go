package wifi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigure(t *testing.T) {
	tests := map[string]struct {
		ssid       string
		passphrase string
		responses  []response
		want       []string
	}{
		"open network": {
			ssid:      "Cafe Wi-Fi",
			responses: okResponses("network id / ssid / bssid / flags\n0\tOther\tany\t", "7", "OK", "OK", "OK", "OK", "OK", "OK"),
			want: commands(
				"list_networks", "add_network", "set_network 7 ssid 436166652057692d4669", "set_network 7 scan_ssid 1", "set_network 7 key_mgmt NONE", "enable_network 7", "select_network 7", "save_config",
			),
		},
		"WPA2 network removes duplicates": {
			ssid:       "IEEE",
			passphrase: "password",
			responses:  okResponses("network id / ssid / bssid / flags\n0\tIEEE\tany\t\n4\tIEEE\tany\t\n5\tOther\tany\t", "OK", "OK", "8", "OK", "OK", "OK", "OK", "OK", "OK"),
			want: commands(
				"list_networks", "remove_network 0", "remove_network 4", "add_network", "set_network 8 ssid 49454545", "set_network 8 scan_ssid 1", "set_network 8 psk f42c6fc52df0ebef9ebb4b90b38a5f902e83fe1b135a70e23aed762e9710a12e", "enable_network 8", "select_network 8", "save_config",
			),
		},
		"hexadecimal PSK is retained": {
			ssid:       "home",
			passphrase: strings.Repeat("A", 64),
			responses:  okResponses("network id / ssid / bssid / flags", "1", "OK", "OK", "OK", "OK", "OK", "OK"),
			want:       commands("list_networks", "add_network", "set_network 1 ssid 686f6d65", "set_network 1 scan_ssid 1", "set_network 1 psk "+strings.Repeat("a", 64), "enable_network 1", "select_network 1", "save_config"),
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			runner := &scriptedRunner{responses: test.responses}
			require.NoError(t, configure(context.Background(), runner, test.ssid, test.passphrase))
			assert.Equal(t, test.want, runner.commands)
		})
	}
}

func TestConfigure_CleansUpAfterEverySetupFailure(t *testing.T) {
	steps := []string{"set_network", "set_network", "set_network", "enable_network", "select_network", "save_config"}
	for failingStep, step := range steps {
		t.Run(step, func(t *testing.T) {
			responses := okResponses("network id / ssid / bssid / flags", "2")
			for i := range steps {
				if i == failingStep {
					responses = append(responses, response{output: "FAIL"})
				} else {
					responses = append(responses, response{output: "OK"})
				}
			}
			responses = append(responses, response{output: "OK"})
			runner := &scriptedRunner{responses: responses}
			err := configure(context.Background(), runner, "home", "password")
			require.Error(t, err)
			require.ErrorContains(t, err, "wpa_cli "+step+" failed")
			assert.Equal(t, command("remove_network 2"), runner.commands[len(runner.commands)-1])
		})
	}
}

func TestConfigure_RejectsInvalidInputWithoutCommands(t *testing.T) {
	for name, test := range map[string]struct{ ssid, passphrase string }{
		"empty SSID":               {passphrase: "password"},
		"short passphrase":         {ssid: "home", passphrase: "short"},
		"long passphrase":          {ssid: "home", passphrase: strings.Repeat("a", 65)},
		"non-hex 64 character PSK": {ssid: "home", passphrase: strings.Repeat("g", 64)},
	} {
		t.Run(name, func(t *testing.T) {
			runner := &scriptedRunner{}
			require.Error(t, configure(context.Background(), runner, test.ssid, test.passphrase))
			assert.Empty(t, runner.commands)
		})
	}
}

func TestConfigure_RejectsInvalidNetworkListWithoutMutation(t *testing.T) {
	runner := &scriptedRunner{responses: []response{{output: "FAIL"}}}
	err := configure(context.Background(), runner, "home", "password")
	require.Error(t, err)
	require.ErrorContains(t, err, "list_networks returned an invalid response")
	assert.Equal(t, []string{command("list_networks")}, runner.commands)
}

func TestConfigure_SurfacesFailureWithoutCredentialLeakage(t *testing.T) {
	const secret = "password"
	runner := &scriptedRunner{responses: []response{{err: errors.New(secret)}}}
	err := configure(context.Background(), runner, "home", secret)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret)
	assert.NotContains(t, err.Error(), "f42")
}

type response struct {
	output string
	err    error
}

type scriptedRunner struct {
	responses []response
	commands  []string
}

func (r *scriptedRunner) Run(_ context.Context, command string) (string, error) {
	r.commands = append(r.commands, command)
	if len(r.responses) == 0 {
		return "", errors.New("unexpected command")
	}
	result := r.responses[0]
	r.responses = r.responses[1:]
	return result.output, result.err
}

func command(verb string) string { return "wpa_cli -i wlan0 -p /data/misc/wifi/sockets " + verb }

func commands(verbs ...string) []string {
	result := make([]string, len(verbs))
	for i, verb := range verbs {
		result[i] = command(verb)
	}
	return result
}

func okResponses(outputs ...string) []response {
	responses := make([]response, len(outputs))
	for i, output := range outputs {
		responses[i] = response{output: output}
	}
	return responses
}
