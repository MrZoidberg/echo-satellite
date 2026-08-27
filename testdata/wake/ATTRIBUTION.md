# Wake test-data attribution

`reference/mel_reference.txt` and `reference/embedding_reference.txt` were
adapted from EchoLocal at commit
`be6b0b00d7d5d765d859b3cbe0e19e127a0c2031` (MIT). EchoLocal generated these
numeric vectors with TensorFlow Lite's reference runtime from deterministic,
synthetic inputs. They contain neither model weights nor recorded audio.

Files under `synthetic/` are generated entirely by this repository's tests.
They contain small analytical constants only and are not third-party models.
