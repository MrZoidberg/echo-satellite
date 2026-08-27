//go:build linux

package mixer

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

func TestControlABI_Matches64BitLinuxUAPI(t *testing.T) {
	t.Parallel()

	assert.Equal(t, uintptr(64), unsafe.Sizeof(elementID{}))
	assert.Equal(t, uintptr(272), unsafe.Sizeof(elementInfo{}))
	assert.Equal(t, uintptr(1224), unsafe.Sizeof(elementValue{}))
	assert.Equal(t, uintptr(0xc1105511), ioctlElementInfo)
	assert.Equal(t, uintptr(0xc4c85512), ioctlElementRead)
	assert.Equal(t, uintptr(0xc4c85513), ioctlElementWrite)
}
