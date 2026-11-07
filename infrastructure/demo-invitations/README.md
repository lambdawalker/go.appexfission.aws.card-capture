# CardDetector demo invitations

A small administrator console for `https://demo.card-detector.apexfission.com/`.
Sign in through the dedicated demo Cognito user pool, enter a tester's email,
and send an invitation. Cognito emails the tester a temporary password that
expires after three days. Self-registration is disabled. Only a user in
`invitation-admins` can create accounts. Ordinary tester accounts have no
invitation access.

## One-time deployment

Use an authenticated AWS CLI profile and AWS SAM CLI. The specified Route 53
public hosted zone must be authoritative for the site name. If the parent
domain is managed elsewhere, delegate the site's subdomain to Route 53 first.
Use the zone that will contain the site alias and ACM validation record.

From this directory, using the bench region and the correct AWS account:

```sh
sam build
sam validate --lint
sam deploy --stack-name carddetector-demo-invitations \
  --region us-east-2 --resolve-s3 --capabilities CAPABILITY_IAM \
  --parameter-overrides HostedZoneId=YOUR_HOSTED_ZONE_ID \
    BenchBucket=YOUR_BENCH_INPUT_BUCKET BenchFunctionArn=YOUR_BENCH_FUNCTION_ARN
```

The stack provisions a certificate, HTTPS API and dedicated user pool. It does
not modify the existing bench deployment. The site's callback and login
configuration come from the stack. Deploying a second hostname requires an
explicit `DomainName` override.

## First administrator

Use the `UserPoolId` stack output and the AWS CLI once. This example sends the
administrator their temporary password by email:

```sh
aws cognito-idp admin-create-user --region us-east-2 \
  --user-pool-id YOUR_USER_POOL_ID --username admin@example.com \
  --user-attributes Name=email,Value=admin@example.com \
  --desired-delivery-mediums EMAIL
aws cognito-idp admin-add-user-to-group --region us-east-2 \
  --user-pool-id YOUR_USER_POOL_ID --username admin@example.com \
  --group-name invitation-admins
```

Visit the site, sign in and choose a new password. From then on, issue tester
invitations on the site. For an expired or undelivered invitation, an operator
can use `admin-create-user --message-action RESEND`. Use
`admin-disable-user` to prevent a tester from signing in again. Cognito's default
email delivery is intended for low-volume testing.

## Independent Android demo

Deploy the isolated bench first. Pass its input bucket and function ARN to this
stack in the same AWS account and region. Deploy a bench revision that supports
`include_extraction`. Run `sam build` to package the worker's Pillow dependency.
Then build/install the Android demo, open **Backend OCR**, sign in with an invited
email, and capture a synthetic card. The workstation can be offline.

The app reads `/android-config.json` and uses a separate public Cognito client
with authorization code, PKCE and `demo-ocr/run` scope. The callback is
`com.apexfission.carddetector.demo:/oauth2redirect`. No client secret is packaged
in the app. Testers have no invitation-administrator privileges.

`POST /api/ocr/jobs` accepts `bytes` and a base64 SHA-256 checksum, and returns an
`id` and a short-lived `uploadUrl`. The signed PUT requires `Content-Type:
image/jpeg`, `Content-Length`, `x-amz-checksum-sha256`, and `If-None-Match: *`.
An upload can only be created once. An S3 notification starts the worker, which
validates the JPEG dimensions and checksum, invokes the existing bench Lambda,
and stores its result. `GET /api/ocr/jobs/{id}` returns `uploading`, `processing`,
`complete`, or `failed`, and the bench `result` when available. The API checks
ownership and expiry on every read, and permits one new job per user per minute.
Duplicate upload notifications do not rerun inference. Jobs stuck after an
interrupted worker report `job_timed_out` after five minutes.

The upload bucket retains synthetic images for one day; DynamoDB results have a
one-day TTL and expiry is also enforced by the API. AWS expiry deletion is
asynchronous. Temporary copies in the bench input bucket are deleted after
inference, with its lifecycle as fallback. Worker concurrency is bounded to two.
The separate demo stack does not participate in the feature's Go package imports.

For deployment validation, sign in as an invited tester, scan, and check that
fields and timings appear. Sign in as a second tester and verify that the first
job ID returns 404. Verify that a tester cannot issue invitations. Live AWS and
Android device checks require the deployed stack and a newly built APK.

## Local checks

```sh
python3 -m pip install boto3 -r ocr/requirements.txt
python3 -m unittest discover -s . -p 'test_*.py'
node --check site/site.js
```
