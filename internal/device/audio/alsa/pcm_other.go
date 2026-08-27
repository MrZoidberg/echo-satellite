//go:build !linux

package alsa

type PCM struct{}

func OpenCapture(Config) (*PCM, error)  { return nil, ErrUnsupportedPlatform }
func OpenPlayback(Config) (*PCM, error) { return nil, ErrUnsupportedPlatform }

func (*PCM) ReadInterleaved([]byte) (int, error)  { return 0, ErrUnsupportedPlatform }
func (*PCM) WriteInterleaved([]byte) (int, error) { return 0, ErrUnsupportedPlatform }
func (*PCM) Prepare() error                       { return ErrUnsupportedPlatform }
func (*PCM) Start() error                         { return ErrUnsupportedPlatform }
func (*PCM) Drop() error                          { return ErrUnsupportedPlatform }
func (*PCM) Close() error                         { return ErrUnsupportedPlatform }
