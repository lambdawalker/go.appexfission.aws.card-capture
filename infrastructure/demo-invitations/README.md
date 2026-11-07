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
sam validate --lint
sam deploy --stack-name carddetector-demo-invitations \
  --region us-east-2 --resolve-s3 --capabilities CAPABILITY_IAM \
  --parameter-overrides HostedZoneId=YOUR_HOSTED_ZONE_ID
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

The dedicated pool is the identity source for the planned standalone Android
demo. The invitation site issues accounts only. The Android public client,
authenticated OCR upload and asynchronous result API are separate work.

## Local checks

```sh
python3 -m unittest discover -s . -p 'test_*.py'
node --check site/site.js
```
