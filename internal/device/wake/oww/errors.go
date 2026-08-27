// Package oww implements openWakeWord's shared feature backbone and classifier engine.
package oww

import "errors"

var (
	ErrMissingSharedModels = errors.New("openwakeword shared models are required")
	ErrInvalidModelShape   = errors.New("openwakeword model has invalid shape")
)
