package system

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Linux exposes process times in USER_HZ units; Linux uses a stable USER_HZ of 100
// across architectures, including Android arm64.
const procClockTicksPerSecond = 100

type Usage struct {
	CPUTime  time.Duration
	RSSBytes uint64
}

func ReadUsage(procRoot string) (Usage, error) {
	stat, err := os.ReadFile(filepath.Join(procRoot, "stat")) //nolint:gosec // G304: procRoot is injected deliberately for tests and alternate process roots.
	if err != nil {
		return Usage{}, fmt.Errorf("read process stat: %w", err)
	}
	cpuTime, err := parseCPUTime(string(stat))
	if err != nil {
		return Usage{}, err
	}
	status, err := os.ReadFile(filepath.Join(procRoot, "status")) //nolint:gosec // G304: procRoot is injected deliberately for tests and alternate process roots.
	if err != nil {
		return Usage{}, fmt.Errorf("read process status: %w", err)
	}
	rss, err := parseRSS(string(status))
	if err != nil {
		return Usage{}, err
	}
	return Usage{CPUTime: cpuTime, RSSBytes: rss}, nil
}

func parseCPUTime(stat string) (time.Duration, error) {
	closingParen := strings.LastIndex(stat, ")")
	if closingParen < 0 || closingParen+1 >= len(stat) {
		return 0, errors.New("parse process stat: missing command field")
	}
	fields := strings.Fields(stat[closingParen+1:])
	if len(fields) <= 12 {
		return 0, errors.New("parse process stat: too few fields")
	}
	userTicks, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse process user CPU time: %w", err)
	}
	systemTicks, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse process system CPU time: %w", err)
	}
	ticks := userTicks + systemTicks
	if ticks < userTicks {
		return 0, errors.New("parse process CPU time: tick count overflow")
	}
	tickDuration := uint64(time.Second / procClockTicksPerSecond)
	if ticks > math.MaxInt64/tickDuration {
		return 0, errors.New("parse process CPU time: duration overflow")
	}
	return time.Duration(ticks * tickDuration), nil //nolint:gosec // G115: the preceding bound check proves the product fits int64.
}

func parseRSS(status string) (uint64, error) {
	for line := range strings.Lines(status) {
		key, value, found := strings.Cut(line, ":")
		if !found || key != "VmRSS" {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) != 2 || fields[1] != "kB" {
			return 0, errors.New("parse process RSS: expected kB value")
		}
		kilobytes, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse process RSS: %w", err)
		}
		if kilobytes > math.MaxUint64/1024 {
			return 0, errors.New("parse process RSS: byte count overflow")
		}
		return kilobytes * 1024, nil
	}
	return 0, errors.New("parse process RSS: VmRSS is missing")
}

type Sampler struct {
	previousUsage Usage
	previousTime  time.Time
	hasPrevious   bool
}

func (s *Sampler) CPUPercent(current Usage, sampledAt time.Time) float64 {
	if !s.hasPrevious {
		s.store(current, sampledAt)
		return 0
	}
	elapsed := sampledAt.Sub(s.previousTime)
	cpuDelta := current.CPUTime - s.previousUsage.CPUTime
	s.store(current, sampledAt)
	if elapsed <= 0 || cpuDelta < 0 {
		return 0
	}
	return float64(cpuDelta) / float64(elapsed) * 100
}

func (s *Sampler) store(usage Usage, sampledAt time.Time) {
	s.previousUsage = usage
	s.previousTime = sampledAt
	s.hasPrevious = true
}
