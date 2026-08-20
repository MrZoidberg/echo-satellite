# Third-party notices

This project is MIT-licensed. The notices below cover third-party code or
model assets used by the Echo Satellite implementation. Do not add adapted code
or model assets without updating this file in the same change.

## EchoLocal

- Upstream: <https://github.com/ygelfand/echolocal>
- License: MIT
- Local license copy: `docs/licenses/echolocal-MIT.txt`
- Use in this repository: code is copied and adapted rather than imported
  because EchoLocal's Go packages live below `internal/`.

Every file adapted from EchoLocal must carry this header near the top of the
file:

```go
// Adapted from github.com/ygelfand/echolocal (MIT). See docs/third-party-notices.md.
```

Planned adapted paths for Milestone 1:

- `internal/device/vec/vec.go`
- `internal/device/vec/vec_arm64.go`
- `internal/device/vec/vec_noasm.go`
- `internal/device/vec/dot_arm64.s`
- `internal/device/vec/axpy_arm64.s`
- `internal/device/audio/alsa/ioctl_linux.go`
- `internal/device/audio/alsa/pcm_linux.go`
- `internal/device/mixer/control_linux.go`
- `internal/device/wake/vadlevel/detector.go`
- `internal/device/wake/tflite/*.go`

The TFLite interpreter and its reference vectors are adapted from EchoLocal
commit `be6b0b00d7d5d765d859b3cbe0e19e127a0c2031`. The generated synthetic
`.tflite` fixtures contain only repository-authored analytical constants; see
`testdata/wake/ATTRIBUTION.md`.

## openWakeWord model assets

- Upstream: <https://github.com/dscripka/openWakeWord>
- License for model weights and shared feature assets: Apache-2.0 unless an
  asset-specific source states otherwise.
- Local asset procedure: `docs/wake-models.md`

The openWakeWord model binaries are not committed to this repository. They are
operator-installed device assets under `/data/local/etc/echo-satellite/wake-models/`
with explicit SHA-256 verification. `echod` never downloads model assets.

## Repository license boundary

The repository remains MIT-only. Do not introduce code or assets that would add
an MPL, GPL, AGPL, or other incompatible licensing obligation without a separate
design and license review.
