# Replacing and training wake models

Echo Satellite treats a wake classifier as a replaceable device asset. Installing
or selecting one does not rebuild `echod` and does not change the shared
melspectrogram model, speech-embedding model, TFLite interpreter, wake pipeline,
or VAD gate. The Dot never downloads a model. Until gateway-managed wake assets
arrive in Milestone 4, an operator stages local files and installs them with
`echoctl`.

## Before installing a model

A candidate must be a TensorFlow Lite openWakeWord classifier whose first input
has shape `[1, frames, 96]`, where `frames` is positive. The 96 values per frame
come from the frozen shared speech-embedding backbone. Prefer openWakeWord's
fully connected (`dnn`) export. Upstream also permits a two-layer RNN classifier,
but this repository's pure-Go interpreter has no recurrent kernels.

The installer validates the complete operator inventory and input shape before
publishing any files. For example, the unsupported `Hey_Prime` classifier tested
during Milestone 1 failed with:

```text
echoctl: install wake model: wake: unsupported model operators: SHAPE, STRIDED_SLICE, REDUCE_PROD, PACK, FILL
```

That is an installation failure, not a prompt to deploy the model and hope it
runs. Export a fully connected classifier instead, or add and test the missing
interpreter kernels in a separately planned runtime change.

Every model also needs strict JSON sidecar metadata. `schema`, `id`, `kind`, and
`sha256` are required. `phrase` and `languages` make `wake list` useful to an
operator and should be supplied. Use a stable ID that describes the model rather
than a mutable filename:

```json
{
  "schema": 1,
  "id": "my_wake-v1",
  "kind": "openwakeword",
  "phrase": "my wake phrase",
  "languages": ["en"],
  "sample_rate": 16000,
  "sha256": "<64-lowercase-hex-characters>",
  "source": "<model source and immutable revision>",
  "license": "<SPDX license identifier>"
}
```

Compute the digest from the exact classifier bytes, then put the same lowercase
value in the sidecar and on the install command:

```sh
sha256sum my_wake.tflite
```

On macOS, use `shasum -a 256 my_wake.tflite`. Replace the example placeholders
with the real values; they are not valid sidecar data. Verify the model's source and
licence before staging it; a digest proves byte identity, not provenance or
quality.

## Install and switch on a Dot

The paths passed to `echoctl` are local to the machine running it. For the
Milestone 1 on-device CLI, first stage the classifier and sidecar on the Dot:

```sh
MODEL_SHA256='<verified-64-character-digest>'
"$ADB" shell mkdir -p /data/local/tmp/models
"$ADB" push my_wake.tflite /data/local/tmp/models/my_wake.tflite
"$ADB" push my_wake.json /data/local/tmp/models/my_wake.json
"$ADB" shell "su -c '/data/local/bin/echoctl wake install my_wake-v1 \
  --from /data/local/tmp/models/my_wake.tflite \
  --metadata /data/local/tmp/models/my_wake.json \
  --sha256 $MODEL_SHA256'"
"$ADB" shell "su -c '/data/local/bin/echoctl wake list'"
```

`wake list` must show the old and new model IDs, kinds, phrases, languages,
sizes, and digest prefixes. Installation is local-only, digest-first, and
atomic. A URL passed to `--from`, a digest mismatch, an incompatible input
shape, or an unsupported operator exits non-zero without selecting a partial
model generation.

Installing does not activate a model. Test the candidate first, using a
threshold measured for that model rather than assuming another classifier's
threshold is transferable:

```sh
"$ADB" shell "su -c '/data/local/bin/echoctl wake test \
  --model my_wake-v1 --threshold 0.5 --seconds 60 --print-steps'"
"$ADB" shell "su -c '/data/local/bin/echoctl wake test \
  --model okay_nabu --threshold 0.5 --seconds 60 --print-steps'"
```

Replace the candidate's example `0.5` only after its threshold sweep; it is not
inherited from `okay_nabu`.

Qualify the complete pipeline before promotion: record wake/VAD alignment, the
chosen wake and VAD thresholds, false accepts, false rejects, multiple speakers,
and representative quiet and noisy rooms. The procedure and evidence fields are
in [device-diagnostics.md](device-diagnostics.md). A successful load or one
spoken phrase is not model qualification.

Select a qualified model for one run with a flag:

```sh
"$ADB" shell "su -c '/data/local/bin/echod --wake-only \
  --wake-model my_wake-v1 --wake-threshold 0.5'"
```

Replace `0.5` with the candidate's qualified threshold.

The Milestone 1 switching demonstration used `hey_rhasspy-v0.1`. A first live
run at wake threshold `0.50` produced no acceptance and a maximum score of
`0.2689`. A second run at candidate threshold `0.20` with `1200 ms` VAD lookback
accepted two deliberate `hey rhasspy` utterances, peaked at `0.9922`, rejected
the spoken `okay nabu` cross-check, and dropped no frames. This proves model
replacement and selection; it does not replace the full qualification corpus
required before making the model a supported default.

Or make the same selection in the `echod` ini file:

```ini
wake-model = my_wake-v1
wake-threshold = <qualified-threshold>
```

Restart `echod` after changing its configuration. Switching back is the same
operation with the previous model ID. Neither direction needs a new binary or a
redeploy of `echod`; only the installed asset and configuration selection
change.

## Train a new phrase upstream

Training is host-side work and is not implemented in this repository. Use the
upstream openWakeWord
[automatic-training notebook](https://github.com/dscripka/openWakeWord/blob/main/notebooks/automatic_model_training.ipynb)
or its linked Google Colab workflow. The upstream process:

1. generates synthetic TTS examples for the target phrase;
2. augments them and combines them with negative speech, noise, and music;
3. converts audio into features using the frozen melspectrogram and Google
   speech-embedding backbone;
4. trains only a small phrase-specific classifier on those embeddings; and
5. exports the classifier as `.tflite`.

Upstream describes the simple Colab path as taking roughly an hour and requiring
no local GPU. That is a quick starting point, not a production-quality promise:
the upstream documentation warns that the simple workflow can perform poorly in
some deployments. More data and the detailed automatic-training workflow may be
needed.

Choose the fully connected/DNN model type. Do not retrain or package replacement
mel or embedding models for each phrase: those are the frozen shared backbone,
which is why adding a wake word requires only a small classifier plus metadata
and no device code change. After export:

1. compute the classifier SHA-256;
2. author its sidecar;
3. run `echoctl wake install` so the installer checks shape and operators;
4. run the on-device acceptance and qualification procedure above; and
5. promote it to configuration only if the recorded results pass.

The authoritative upstream overview is openWakeWord's
[Training New Models](https://github.com/dscripka/openWakeWord#training-new-models)
section. Repository-vetted downloadable assets and their pinned digests are in
[wake-models.md](wake-models.md).

## Distribution boundary

Milestone 1 has exactly one installation path: operator-provided local files via
`echoctl`. The Dot must not fetch from GitHub, Hugging Face, HTTP, or any other
remote source. Gateway-driven, authenticated wake-asset synchronization belongs
to Milestone 4 and must preserve the same metadata, digest, compatibility, and
atomic-publication checks.
