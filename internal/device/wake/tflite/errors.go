// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.

package tflite

import "errors"

var (
	// ErrUnsupportedOp reports an operator that this deliberately small runtime cannot execute.
	ErrUnsupportedOp = errors.New("tflite: unsupported operator")
	// ErrBadModel reports malformed or internally inconsistent FlatBuffer model data.
	ErrBadModel = errors.New("tflite: bad model")
	// ErrShapeMismatch reports incompatible tensor shapes or invalid shape metadata.
	ErrShapeMismatch = errors.New("tflite: shape mismatch")
)
