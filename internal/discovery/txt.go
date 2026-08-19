package discovery

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// TXT record keys defined for the satellite service.
const (
	txtKeyProtocol = "protocol"
	txtKeyServerID = "server_id"
	txtKeyTLS      = "tls"
	txtKeyPath     = "path"
)

// secretKeyFragments are substrings that must never appear in a TXT key. mDNS
// is unauthenticated and world-readable on the segment, so a record carrying
// anything credential-shaped is treated as hostile or misconfigured rather than
// quietly accepted.
var secretKeyFragments = []string{"token", "secret", "key", "password", "passwd", "psk", "auth", "cred"}

// Errors returned while parsing or validating a TXT record.
var (
	// ErrSecretInTXT is returned when a TXT record carries a credential-shaped key.
	ErrSecretInTXT = errors.New("discovery: TXT record must not carry credentials")
	// ErrMissingServerID is returned when a TXT record omits the server identity.
	ErrMissingServerID = errors.New("discovery: TXT record has no server_id")
	// ErrInvalidProtocol is returned when a TXT record announces no usable protocol version.
	ErrInvalidProtocol = errors.New("discovery: TXT record has an invalid protocol version")
)

// TXTRecord is the discovery metadata a gateway advertises. It contains no
// secrets by construction: Encode emits only these four keys, and ParseTXT
// rejects any record that carries a credential-shaped key.
type TXTRecord struct {
	Protocol int
	ServerID string
	TLS      bool
	Path     string
}

// Encode renders the record as DNS-SD key=value strings, in stable order.
func (r TXTRecord) Encode() []string {
	path := r.Path
	if path == "" {
		path = DefaultPath
	}
	tls := "0"
	if r.TLS {
		tls = "1"
	}
	return []string{
		txtKeyProtocol + "=" + strconv.Itoa(r.Protocol),
		txtKeyServerID + "=" + r.ServerID,
		txtKeyTLS + "=" + tls,
		txtKeyPath + "=" + path,
	}
}

// Validate checks the record is usable for connecting to a gateway.
func (r TXTRecord) Validate() error {
	if r.ServerID == "" {
		return ErrMissingServerID
	}
	if r.Protocol <= 0 {
		return fmt.Errorf("%w: %d", ErrInvalidProtocol, r.Protocol)
	}
	if r.Path != "" && !strings.HasPrefix(r.Path, "/") {
		return fmt.Errorf("discovery: TXT path %q must start with /", r.Path)
	}
	return nil
}

// ParseTXT reads a DNS-SD TXT record. Unknown keys are ignored so the record
// can grow, but a credential-shaped key fails the whole record.
func ParseTXT(entries []string) (TXTRecord, error) {
	var rec TXTRecord
	for _, entry := range entries {
		key, value, found := strings.Cut(entry, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		if fragment, secret := secretLikeKey(key); secret {
			return TXTRecord{}, fmt.Errorf("%w: key %q looks like %q", ErrSecretInTXT, key, fragment)
		}
		if !found {
			continue
		}

		switch key {
		case txtKeyProtocol:
			n, err := strconv.Atoi(value)
			if err != nil {
				return TXTRecord{}, fmt.Errorf("%w: %q", ErrInvalidProtocol, value)
			}
			rec.Protocol = n
		case txtKeyServerID:
			rec.ServerID = value
		case txtKeyTLS:
			rec.TLS = value == "1" || strings.EqualFold(value, "true")
		case txtKeyPath:
			rec.Path = value
		default:
			// unknown keys are ignored on purpose: a newer gateway may advertise more
		}
	}

	if rec.Path == "" {
		rec.Path = DefaultPath
	}
	return rec, nil
}

func secretLikeKey(key string) (fragment string, secret bool) {
	for _, f := range secretKeyFragments {
		if strings.Contains(key, f) {
			return f, true
		}
	}
	return "", false
}
