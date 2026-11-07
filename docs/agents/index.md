# Card capture integration and operations

Scope: current Go source. For system requirements see [Attestra journeys](https://raw.githubusercontent.com/lambdawalker/design.attestra/main/docs/agents/journeys.md).
For composition and rollout see the [application guide](https://raw.githubusercontent.com/lambdawalker/go.attestra.aws/main/docs/card-capture-composition.md).
For client lifecycle see the [Android recipe](https://raw.githubusercontent.com/lambdawalker/android.attestra.auth/main/docs/agents/recipes.md).

## Ownership and entry points

The runtime owns private upload registration, frozen S3 object validation,
extraction, same-capture retakes and review persistence. The separate
infrastructure module creates its AWS resources. Hosts use the
standard-library-only [consent contracts](../../contracts/policy.go) and
[composition guide](../../README.md). The Go auth feature supplies the identity
and reset integration, and the application chooses environment and module pins.

## Client sequence

All API routes below are under `/onboarding/id`. See the
[HTTP handler](../../captureapi/handler.go) and [request and record models](../../capture/model.go)
for exact serialization. Use authenticated HTTPS for the API and the returned
presigned URL and headers for each private S3 PUT.

1. Read `GET /document-policy` and present consent before `POST /captures`.
2. Register each JPEG with `POST /captures/{id}/uploads` and upload the bytes.
3. Finalize with `POST /captures/{id}/finalize` using the selected upload IDs.
   The worker validates the frozen evidence and runs OCR asynchronously.
4. Poll `GET /captures/{id}`. Match both `capture_id` and `image_revision`
   before showing results for the confirmed photos. The record `revision`
   changes as processing advances and is used for optimistic concurrency.
5. Save corrections with `POST /captures/{id}/review`. The wire key
   `issued_identifier` represents the issuer-assigned personal identifier.

A retake calls `POST /captures/{id}/retake` with `operation_key` and
`expected_revision`. The same capture ID stays in use. The worker generation
changes, invalidating older work, and replacement uploads receive a new image
revision at finalization. Reconcile uncertain responses before retrying. See the
[retake implementation](../../capture/retake.go).

## OCR timing

The live worker sends two color images to Gemma with `medium` reasoning effort.
The [isolated benchmark](../ocr-bench.md) also uses medium and can send one
stitched grayscale image. Compare like-for-like inputs before attributing a
latency change to reasoning effort.

In CloudWatch, find `capture.work_started` and `capture.work_finished` for
`work=extract` and the capture ID. Use the invocation context to find
`capture.inference_prepared` (`elapsed_ms`) and `capture.inference_response`
(`headers_ms`). Worker duration includes storage and processing. Header timing
measures the provider request through response headers. Android's `ocr_observed`
also includes time between status polls and network delay. Repository revisions
and benchmark runs do not establish the code version or latency in an AWS environment.


## Early grayscale upload and stitched evidence

The policy response advertises `stitched_pair_supported` independently of its
consent hash. A client can register and upload the grayscale `front` immediately,
then call `POST /onboarding/id/captures/{capture_id}/confirm-front` with its
`upload_id` after the PUT succeeds. Confirmation pins the object version and
queues OCR immediately. Finalization starts front OCR if confirmation was missed.
The worker sends just the front image to OCR in grayscale
with a maximum edge of 1600 pixels. Android can upload the color `stitched` JPEG
and finalize both IDs while OCR runs. The worker validates and retains both
evidence files. Legacy clients can still finalize `front` plus `back`. A capture
generation cannot mix `back` and `stitched`.

After the first OCR failure, the extraction status becomes `retrying`. A ready
capture can be reviewed and saved at this point. The worker queues one immediate
retry and sets `complete` or `failed` when it finishes.

Each registered upload has a unique object key. Successful PUTs remain in the
private versioned evidence bucket even after a retake clears the selected IDs.
Only the selected front is eligible for early OCR. Finalization validates the
selected evidence pair. Retention expiry, cancellation, reset and orphan cleanup still
remove evidence under their existing rules.
