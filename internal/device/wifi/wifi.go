// Package wifi configures the Android-owned wpa_supplicant service.
package wifi

import (
	"context"
	"crypto/pbkdf2"
	"crypto/sha1" //nolint:gosec // G505: WPA2-PSK derivation mandates PBKDF2-SHA1.
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

const (
	interfaceName = "wlan0"
	controlDir    = "/data/misc/wifi/sockets"
)

// Set creates and selects the requested open or WPA2-PSK network. It never
// passes a plaintext passphrase to the root shell.
func Set(ctx context.Context, ssid, passphrase string) error {
	return configure(ctx, shellRunner{}, ssid, passphrase)
}

type commandRunner interface {
	Run(context.Context, string) (string, error)
}

type shellRunner struct{}

func (shellRunner) Run(ctx context.Context, command string) (string, error) {
	output, err := exec.CommandContext(ctx, "su", "-c", command).CombinedOutput() //nolint:gosec // command contains fixed wpa_cli verbs plus validated numeric or hexadecimal values.
	return string(output), err
}

func configure(ctx context.Context, runner commandRunner, ssid, passphrase string) error {
	if err := validateSSID(ssid); err != nil {
		return err
	}
	psk, err := derivePSK(ssid, passphrase)
	if err != nil {
		return err
	}

	configured, err := listNetworks(ctx, runner)
	if err != nil {
		return err
	}
	for _, network := range configured {
		if network.ssid == ssid {
			if removeErr := expectOK(ctx, runner, "remove_network", strconv.Itoa(network.id)); removeErr != nil {
				return removeErr
			}
		}
	}

	id, err := addNetwork(ctx, runner)
	if err != nil {
		return err
	}
	if err := configureNetwork(ctx, runner, id, ssid, psk); err != nil {
		cleanupNetwork(ctx, runner, id)
		return err
	}
	return nil
}

func configureNetwork(ctx context.Context, runner commandRunner, id int, ssid, psk string) error {
	networkID := strconv.Itoa(id)
	if err := expectOK(ctx, runner, "set_network", networkID, "ssid", hex.EncodeToString([]byte(ssid))); err != nil {
		return err
	}
	if err := expectOK(ctx, runner, "set_network", networkID, "scan_ssid", "1"); err != nil {
		return err
	}
	if psk == "" {
		if err := expectOK(ctx, runner, "set_network", networkID, "key_mgmt", "NONE"); err != nil {
			return err
		}
	} else if err := expectOK(ctx, runner, "set_network", networkID, "psk", psk); err != nil {
		return err
	}
	if err := expectOK(ctx, runner, "enable_network", networkID); err != nil {
		return err
	}
	if err := expectOK(ctx, runner, "select_network", networkID); err != nil {
		return err
	}
	return expectOK(ctx, runner, "save_config")
}

func cleanupNetwork(ctx context.Context, runner commandRunner, id int) {
	_ = expectOK(ctx, runner, "remove_network", strconv.Itoa(id))
}

func addNetwork(ctx context.Context, runner commandRunner) (int, error) {
	output, err := run(ctx, runner, "add_network")
	if err != nil {
		return 0, err
	}
	id, err := strconv.Atoi(strings.TrimSpace(output))
	if err != nil || id < 0 {
		return 0, errors.New("wpa_cli add_network returned an invalid network ID")
	}
	return id, nil
}

func listNetworks(ctx context.Context, runner commandRunner) ([]network, error) {
	output, err := run(ctx, runner, "list_networks")
	if err != nil {
		return nil, err
	}
	var networks []network
	headerFound := false
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		if line == "network id / ssid / bssid / flags" {
			headerFound = true
			continue
		}
		if !headerFound {
			return nil, errors.New("wpa_cli list_networks returned an invalid response")
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 4 {
			return nil, errors.New("wpa_cli list_networks returned an invalid network record")
		}
		id, parseErr := strconv.Atoi(fields[0])
		if parseErr != nil || id < 0 {
			return nil, errors.New("wpa_cli list_networks returned an invalid network ID")
		}
		networks = append(networks, network{id: id, ssid: fields[1]})
	}
	if !headerFound {
		return nil, errors.New("wpa_cli list_networks returned an invalid response")
	}
	return networks, nil
}

type network struct {
	id   int
	ssid string
}

func expectOK(ctx context.Context, runner commandRunner, verb string, args ...string) error {
	output, err := run(ctx, runner, verb, args...)
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != "OK" {
		return commandError{operation: verb}
	}
	return nil
}

func run(ctx context.Context, runner commandRunner, verb string, args ...string) (string, error) {
	command := strings.Join(append([]string{"wpa_cli", "-i", interfaceName, "-p", controlDir, verb}, args...), " ")
	output, err := runner.Run(ctx, command)
	if err != nil {
		return "", commandError{operation: verb, cause: err}
	}
	return output, nil
}

type commandError struct {
	operation string
	cause     error
}

func (e commandError) Error() string { return "wpa_cli " + e.operation + " failed" }

func (e commandError) Unwrap() error { return e.cause }

func validateSSID(ssid string) error {
	if ssid == "" {
		return errors.New("SSID must not be empty")
	}
	return nil
}

func derivePSK(ssid, passphrase string) (string, error) {
	if passphrase == "" {
		return "", nil
	}
	if len(passphrase) == 64 {
		decoded, err := hex.DecodeString(passphrase)
		if err == nil && len(decoded) == 32 {
			return strings.ToLower(passphrase), nil
		}
	}
	if len(passphrase) < 8 || len(passphrase) > 63 {
		return "", errors.New("WPA2 passphrase must be 8 to 63 characters or a 64-character hexadecimal PSK")
	}
	key, err := pbkdf2.Key(sha1.New, passphrase, []byte(ssid), 4096, 32)
	if err != nil {
		return "", fmt.Errorf("derive WPA2 PSK: %w", err)
	}
	return hex.EncodeToString(key), nil
}
