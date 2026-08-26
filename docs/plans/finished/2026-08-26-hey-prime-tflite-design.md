# Hey_Prime TFLite compatibility design

**Status:** finished
**Owner:** Codex (/root)
**Created:** 2026-08-26
**Completed:** 2026-08-26

## Objective

Run the operator-provided `.assets/wake-models/Hey_Prime_20260824_084713.tflite`
classifier entirely on the Echo Dot through the existing device-local,
pure-Go openWakeWord pipeline. The installed model is identified as
`hey_prime`, describes the phrase "Hey Prime" in English, and does not replace
the qualified `okay_nabu` default.

## Constraints

- No cgo, external runtime, Python process, network model fetch, gateway wake
  processing, or continuous microphone upload.
- Preserve the current digest-first, local-only, atomic `wake install` flow.
- The supplied model bytes have SHA-256
  `ad1fedb27dac6b9f3401da64f696351e1516a703038eed2c2414ae1740af34f0`.
- Preserve its supplied provenance URL `https://openwakeword.com` and terms
  URL `https://openwakeword.com/terms` verbatim. The currently accessible
  terms page does not identify an SPDX licence, so documentation must not
  assert a licence class that has not been verified.
- `okay_nabu` remains the only qualified default until Hey_Prime completes the
  existing real-device wake-model qualification procedure.

## Current incompatibility

The model has a compatible openWakeWord classifier input shape, but requires
five TFLite operations absent from the interpreter: `SHAPE`, `STRIDED_SLICE`,
`REDUCE_PROD`, `PACK`, and `FILL`. This is an interpreter gap, not an asset
installation or selection gap.

## Chosen approach

Implement the five generic operations in `internal/device/wake/tflite` and
register their schema opcode values. The implementation remains bounded to the
types and semantics the model uses, validates rank/axis/shape inputs before
indexing, and returns interpreter errors for unsupported tensor types or
invalid dynamic values.

- `SHAPE` emits an integer tensor of the input dimensions.
- `STRIDED_SLICE` applies begin/end/stride integer vectors, including valid
  negative indexing and negative strides.
- `REDUCE_PROD` follows the existing reduction convention, including axes and
  `keep_dims` handling.
- `PACK` stacks equally shaped input tensors at a validated axis.
- `FILL` creates a tensor of a validated integer shape filled with its scalar
  value.

Existing `vec.Dot` and `vec.AXPY` assembly paths remain in use where their
contiguous numeric work already applies. These five kernels are control,
indexing, or small-shape operations; no speculative SIMD implementation will
be added without a measured hot-path benefit.

## Validation and delivery

1. Add focused kernel tests for normal, boundary, and invalid input semantics,
   and update opcode/schema tests.
2. Add an asset-gated end-to-end test that parses and prepares the supplied
   Hey_Prime model, exercising it with valid embeddings when the local asset is
   available. CI continues to skip it cleanly without untracked model bytes.
3. Add an explicit strict sidecar example and installation guidance with the
   supplied source/terms URLs; do not misrepresent the unverified licence as
   SPDX or mark the model qualified.
4. Amend the active Milestone 1 plan with a new task covering the runtime
   extension, host checks, installation, and the remaining on-device
   qualification evidence. Task 25 remains documentation closeout and is not
   marked complete by this work alone.
5. Run package tests, portability/static-build checks, formatting, lint, and
   the project test suite. A Dot run will verify that the installer accepts the
   model and will record model-specific threshold and qualification evidence;
   host tests cannot establish wake accuracy.

## Non-goals

- Retraining, converting, or changing Hey_Prime model weights.
- Promoting Hey_Prime to the default model without real-device qualification.
- General recurrent-model support or an external TFLite/ONNX runtime.
- Gateway-managed wake asset distribution.
