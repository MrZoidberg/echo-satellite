//go:build !linux

package main

import "errors"

type statSpace struct{}

func (statSpace) Available(string) (int64, error) {
	return 0, errors.New("update filesystem space is unavailable on this platform")
}
