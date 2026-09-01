package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigureWiFi(t *testing.T) {
	t.Run("reports configured SSID without passphrase", func(t *testing.T) {
		var report bytes.Buffer
		var gotSSID, gotPassphrase string
		err := configureWiFi(&report, wifiSetCommand{SSID: "home", Passphrase: "secret-passphrase"}, func(_ context.Context, ssid, passphrase string) error {
			gotSSID, gotPassphrase = ssid, passphrase
			return nil
		})
		require.NoError(t, err)
		assert.Equal(t, "home", gotSSID)
		assert.Equal(t, "secret-passphrase", gotPassphrase)
		assert.Equal(t, "wifi: configured SSID home\n", report.String())
		assert.NotContains(t, report.String(), gotPassphrase)
	})

	t.Run("wraps configuration failure without passphrase", func(t *testing.T) {
		const secret = "secret-passphrase"
		var report bytes.Buffer
		err := configureWiFi(&report, wifiSetCommand{SSID: "home", Passphrase: secret}, func(context.Context, string, string) error {
			return errors.New("wpa_cli set_network failed")
		})
		require.Error(t, err)
		require.ErrorContains(t, err, "configure Wi-Fi")
		assert.NotContains(t, err.Error(), secret)
		assert.Empty(t, report.String())
	})
}
