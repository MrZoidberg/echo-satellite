package main

import (
	"context"
	"fmt"
	"io"

	"github.com/MrZoidberg/echo-satellite/internal/device/wifi"
)

func wifiSet(w io.Writer, c wifiSetCommand) error {
	return configureWiFi(w, c, wifi.Set)
}

func configureWiFi(w io.Writer, c wifiSetCommand, set func(context.Context, string, string) error) error {
	if err := set(context.Background(), c.SSID, c.Passphrase); err != nil {
		return fmt.Errorf("configure Wi-Fi: %w", err)
	}
	return writeReport(w, []string{"wifi: configured SSID " + c.SSID})
}
