# Feature ownership

Card capture is independent of authentication and applications. Never import go.apexfission.aws.auth or go.attestra.aws. Share only the pure standard-library contracts package with hosts. The runtime module and optional infrastructure module have separate manifests.

Preserve consent policy wire fields/hash, API paths, storage IDs, immediate SQS dispatch, OCR recovery, reset gating and IAM conditions. Run Go tests/race/vet in root and infrastructure; build all three Lambda archives. Do not run AWS deployment, teardown, seed-runtime, or package publication as validation. Never commit generated dist, stack settings, credentials or live outputs.
