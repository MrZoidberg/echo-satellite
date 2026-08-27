package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogValue_RemovesRecordSeparators(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "gateway.example:8443", logValue("gateway.example\r\n:8443"))
}

func TestLogValues_RemovesRecordSeparators(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"id=home", "proto=1"}, logValues([]string{"id=home\n", "proto=1\r"}))
}
