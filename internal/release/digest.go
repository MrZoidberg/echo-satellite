package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Errors returned while checking artifact integrity.
var (
	// ErrSizeMismatch is returned when the artifact is not the length the manifest declares.
	ErrSizeMismatch = errors.New("release: artifact size does not match manifest")
	// ErrDigestMismatch is returned when the artifact bytes do not hash to the manifest digest.
	ErrDigestMismatch = errors.New("release: artifact digest does not match manifest")
)

// Digest streams r and returns its lowercase hex SHA-256 and byte count. The
// artifact is never buffered in memory: a device stages it to a .part file and
// hashes it on the way through.
func Digest(r io.Reader) (digest string, size int64, err error) {
	h := sha256.New()
	n, err := io.Copy(h, r)
	if err != nil {
		return "", 0, fmt.Errorf("release: read artifact: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// VerifyArtifact checks the artifact behind r against the manifest. Size and
// digest failures are distinct errors: a short read is usually a truncated
// download worth retrying, while a digest mismatch on a complete file means the
// bytes are wrong and the release must not be staged.
func VerifyArtifact(r io.Reader, m Manifest) error {
	digest, size, err := Digest(r)
	if err != nil {
		return err
	}
	if size != m.Size {
		return fmt.Errorf("%w: got %d bytes, want %d", ErrSizeMismatch, size, m.Size)
	}
	if !strings.EqualFold(digest, m.SHA256) {
		return fmt.Errorf("%w: got %s, want %s", ErrDigestMismatch, digest, m.SHA256)
	}
	return nil
}
