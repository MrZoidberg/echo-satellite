# Wake model assets

Wake models are explicit device assets. `echod` never fetches them and
`echoctl wake install` must reject URLs; the operator provides local files and
the expected SHA-256 digest at install time.

The runtime uses openWakeWord-style assets in Milestone 1:

- a shared melspectrogram feature model;
- a shared speech embedding model;
- one active wake classifier;
- an optional second classifier for model switching diagnostics.

Prefer a fully-connected openWakeWord classifier export. Upstream also supports
RNN classifiers, but Milestone 1's pure-Go interpreter validates the opcode
inventory at install time and is not expected to run recurrent kernels.

## Vetted assets

| Model ID | Kind | Phrase | Languages | Upstream URL | License | SHA-256 |
|---|---|---|---|---|---|---|
| `oww-melspectrogram-v0.5.1` | openWakeWord feature | n/a | language independent | <https://github.com/dscripka/openWakeWord/releases/download/v0.5.1/melspectrogram.tflite> | Apache-2.0 | `96fa0adccb6e8cf95cb14465409a1a2898ee4a96a85bb9ed3c7eb0e68bf163e8` |
| `oww-embedding-v0.5.1` | openWakeWord feature | n/a | multilingual speech embedding | <https://github.com/dscripka/openWakeWord/releases/download/v0.5.1/embedding_model.tflite> | Apache-2.0 | `c0aea21eb84a4ce90a08c870da41b7a7173b45269e6a3207c71d67c40f3a59d8` |
| `okay_nabu-pyoww-6bc5c5f-20260820` | openWakeWord classifier | "okay nabu" | English phrase | <https://raw.githubusercontent.com/rhasspy/pyopen-wakeword/6bc5c5f5c9c71e46a723b6c9277b1d50f2ba13fd/pyopen_wakeword/models/okay_nabu.tflite> | Apache-2.0 | `2982cecde4ee81cc7a2573d2602a7d54f0669425c94a7b64af77e0ff92b03a18` |
| `hey_jarvis-v0.1` | openWakeWord classifier | "hey jarvis" | English phrase | <https://github.com/dscripka/openWakeWord/releases/download/v0.5.1/hey_jarvis_v0.1.tflite> | Apache-2.0 | `14bff778604985e1b5c19f0f7bbe477a69cf281d8db34b232b3b972411f710e2` |

The `okay_nabu` digest is pinned to the bytes fetched on 2026-08-20 from
rhasspy/pyopen-wakeword commit `6bc5c5f5c9c71e46a723b6c9277b1d50f2ba13fd`.
If the upstream repository changes, use the recorded digest as the trust
boundary and update this table only after deliberate re-vetting.

## Host download and verification

Download into an untracked host directory:

```sh
mkdir -p .assets/wake-models
curl -L -o .assets/wake-models/melspectrogram.tflite \
  https://github.com/dscripka/openWakeWord/releases/download/v0.5.1/melspectrogram.tflite
curl -L -o .assets/wake-models/embedding_model.tflite \
  https://github.com/dscripka/openWakeWord/releases/download/v0.5.1/embedding_model.tflite
curl -L -o .assets/wake-models/okay_nabu.tflite \
  https://raw.githubusercontent.com/rhasspy/pyopen-wakeword/6bc5c5f5c9c71e46a723b6c9277b1d50f2ba13fd/pyopen_wakeword/models/okay_nabu.tflite
curl -L -o .assets/wake-models/hey_jarvis_v0.1.tflite \
  https://github.com/dscripka/openWakeWord/releases/download/v0.5.1/hey_jarvis_v0.1.tflite
sha256sum .assets/wake-models/*.tflite
```

Compare every printed digest to the table above before pushing files to a
device.

## Device install flow

Milestone 1 installs from local paths only. The final `echoctl wake install`
implementation will validate the digest and TFLite opcode inventory before it
writes the model sidecar under `/data/local/etc/echo-satellite/wake-models/`.

Create strict sidecar metadata next to each classifier. Unknown fields are rejected,
and `schema`, `id`, `kind`, and `sha256` are required. For example:

```json
{
  "schema": 1,
  "id": "okay_nabu-pyoww-6bc5c5f-20260820",
  "kind": "openwakeword",
  "phrase": "okay nabu",
  "languages": ["en"],
  "sample_rate": 16000,
  "sha256": "2982cecde4ee81cc7a2573d2602a7d54f0669425c94a7b64af77e0ff92b03a18",
  "source": "rhasspy/pyopen-wakeword@6bc5c5f5c9c71e46a723b6c9277b1d50f2ba13fd",
  "license": "Apache-2.0"
}
```

Install the shared mel and embedding assets under the same model directory using
the fixed names `melspectrogram.tflite` and `embedding_model.tflite`. Then install
the classifier from a local path:

```sh
echoctl wake install okay_nabu-pyoww-6bc5c5f-20260820 \
  --from .assets/wake-models/okay_nabu.tflite \
  --metadata .assets/wake-models/okay_nabu.json \
  --sha256 2982cecde4ee81cc7a2573d2602a7d54f0669425c94a7b64af77e0ff92b03a18 \
  --model-dir ./device-wake-models
echoctl wake list --model-dir ./device-wake-models
```

`--sha256` may be omitted when the sidecar carries it. The installer verifies the
digest before creating the destination directory, parses the TFLite graph, checks
that its input shape agrees with the declared engine kind, rejects unsupported
operators, and only then fsyncs and atomically promotes `.part` files. Classifier
and sidecar generations use the full digest in their immutable filenames; an
atomically replaced `index.json` is the commit point. Overwrite therefore keeps
the previous indexed generation intact through any pre-commit failure. Installs
are serialized with a store lock so `--overwrite=false` cannot race another
installer. After host-side installation, push the generated model directory to
the Dot with ADB or let the later gateway asset-sync milestone manage distribution.
Do not teach `echod` to fetch from HTTP, GitHub, Hugging Face, or any other network
source.
