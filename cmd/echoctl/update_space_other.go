//go:build !linux && !darwin

package main

import "errors"

type statSpace struct{}

func (statSpace) Available(string) (int64, error) {
	return 0, errors.New("local update installation is supported only on Unix devices")
}
