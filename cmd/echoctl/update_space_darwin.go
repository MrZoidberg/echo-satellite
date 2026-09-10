//go:build darwin

package main

import (
	"errors"
	"fmt"
	"math/big"
	"syscall"
)

type statSpace struct{}

func (statSpace) Available(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("stat filesystem: %w", err)
	}
	if stat.Bsize == 0 {
		return 0, errors.New("filesystem block size is not positive")
	}
	available := new(big.Int).Mul(new(big.Int).SetUint64(uint64(stat.Bsize)), new(big.Int).SetUint64(stat.Bavail))
	if !available.IsInt64() {
		return 0, errors.New("filesystem free-space value overflows")
	}
	return available.Int64(), nil
}
