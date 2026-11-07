"""Same-origin invitation console. API Gateway verifies tokens before invoking API routes."""

import base64
import json
import os
from pathlib import Path
import re

ADMIN_GROUP = "invitation-admins"
ASSETS = {
    "/": ("index.html", "text/html; charset=utf-8"),
    "/site.js": ("site.js", "text/javascript; charset=utf-8"),
    "/site.css": ("site.css", "text/css; charset=utf-8"),
}


def response(status, body, content_type="application/json"):
    return {
        "statusCode": status,
        "headers": {
            "content-type": content_type,
            "cache-control": "no-store",
            "x-content-type-options": "nosniff",
            "referrer-policy": "no-referrer",
            "content-security-policy": (
                "default-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; "
                f"connect-src 'self' {os.environ['DEMO_LOGIN']}"
            ),
        },
        "body": json.dumps(body) if content_type == "application/json" else body,
    }


def is_admin(claims):
    groups = claims.get("cognito:groups", [])
    if isinstance(groups, str):
        # API Gateway can serialize the Cognito group list into a string.
        groups = groups.strip("[]").replace('"', "").split(",")
        groups = [group.strip() for group in groups]
    return claims.get("token_use") == "access" and isinstance(groups, list) and ADMIN_GROUP in groups


def invite(event):
    raw = event.get("body") or ""
    if len(raw) > 2048:
        return response(400, {"error": "invalid_email"})
    try:
        if event.get("isBase64Encoded"):
            raw = base64.b64decode(raw, validate=True)
        data = json.loads(raw)
        email = data["email"].strip().lower()
    except (ValueError, TypeError, KeyError, AttributeError):
        return response(400, {"error": "invalid_email"})
    if len(email) > 254 or not re.fullmatch(r"[^\s@]+@[^\s@]+\.[^\s@]+", email):
        return response(400, {"error": "invalid_email"})

    import boto3
    from botocore.exceptions import ClientError

    try:
        boto3.client("cognito-idp").admin_create_user(
            UserPoolId=os.environ["DEMO_POOL_ID"],
            Username=email,
            UserAttributes=[{"Name": "email", "Value": email}],
            DesiredDeliveryMediums=["EMAIL"],
        )
    except ClientError as error:
        code = error.response["Error"]["Code"]
        if code == "UsernameExistsException":
            return response(409, {"error": "already_invited"})
        if code in {"LimitExceededException", "TooManyRequestsException"}:
            return response(429, {"error": "try_later"})
        return response(502, {"error": "invitation_failed"})
    return response(201, {"status": "invitation_requested"})


def handle(event, _context):
    method = event["requestContext"]["http"]["method"]
    path = event["rawPath"]
    if method == "GET" and path in ASSETS:
        file, content_type = ASSETS[path]
        return response(200, Path(__file__).with_name(file).read_text(encoding="utf-8"), content_type)
    if method == "GET" and path == "/android-config.json":
        return response(200, {
            "login": os.environ["DEMO_LOGIN"],
            "clientId": os.environ["DEMO_ANDROID_CLIENT_ID"],
            "redirect": "com.apexfission.carddetector.demo:/oauth2redirect",
        })
    if method == "GET" and path == "/config.json":
        return response(200, {
            "login": os.environ["DEMO_LOGIN"],
            "clientId": os.environ["DEMO_CLIENT_ID"],
            "redirect": os.environ["DEMO_ORIGIN"] + "/",
        })
    if (method, path) not in {("POST", "/api/invitations"), ("GET", "/api/session")}:
        return response(404, {"error": "not_found"})
    claims = event["requestContext"].get("authorizer", {}).get("jwt", {}).get("claims", {})
    if not is_admin(claims):
        return response(403, {"error": "admin_required"})
    if method == "GET":
        return response(200, {"admin": True})
    return invite(event)
