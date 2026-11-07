"""Demo-only OCR API and upload worker. API Gateway validates scoped access tokens."""
import base64
import hashlib
import io
import json
import logging
import os
import time
import uuid
from decimal import Decimal
from urllib.parse import unquote_plus

import boto3
from botocore.config import Config
from botocore.exceptions import ClientError
from PIL import Image, UnidentifiedImageError

MAX_BYTES = 500 * 1024


def table():
    return boto3.resource("dynamodb").Table(os.environ["JOB_TABLE"])


def response(status, body):
    return {"statusCode": status, "headers": {"content-type": "application/json", "cache-control": "no-store"},
            "body": json.dumps(body, default=lambda n: int(n) if isinstance(n, Decimal) else str(n))}


def api(event, _context):
    claims = event.get("requestContext", {}).get("authorizer", {}).get("jwt", {}).get("claims", {})
    owner = claims.get("sub")
    if not owner or claims.get("token_use") != "access":
        return response(401, {"error": "sign_in_required"})
    now = int(time.time())
    db = table()
    if event["requestContext"]["http"]["method"] == "GET":
        job = db.get_item(Key={"id": event.get("pathParameters", {}).get("id", "")}, ConsistentRead=True).get("Item")
        if not job or job.get("owner") != owner or job.get("expires", 0) <= now:
            return response(404, {"error": "job_not_found"})
        state = job["state"]
        if state in {"uploading", "processing"} and now - job["created"] > 300:
            return response(200, {"state": "failed", "error": "job_timed_out"})
        return response(200, {"state": state, "result": json.loads(job["result"]) if "result" in job else None})
    try:
        raw = event.get("body", "")
        if len(raw) > 2048:
            raise ValueError()
        if event.get("isBase64Encoded"):
            raw = base64.b64decode(raw, validate=True)
        data = json.loads(raw)
        size = data["bytes"]
        checksum = data["sha256"]
        if type(size) is not int or not 0 < size <= MAX_BYTES or len(base64.b64decode(checksum, validate=True)) != 32:
            raise ValueError()
    except (ValueError, TypeError, KeyError):
        return response(400, {"error": "invalid_image_metadata"})
    # One new job per tester per minute. Conditional writes enforce this across requests.
    try:
        db.put_item(Item={"id": "limit/" + owner, "next": now + 60, "expires": now + 86400},
                    ConditionExpression="attribute_not_exists(id) OR #next <= :now",
                    ExpressionAttributeNames={"#next": "next"}, ExpressionAttributeValues={":now": now})
    except ClientError as error:
        if error.response["Error"]["Code"] == "ConditionalCheckFailedException":
            return response(429, {"error": "wait_one_minute"})
        raise
    job_id = uuid.uuid4().hex
    key = "inputs/" + job_id + ".jpg"
    db.put_item(Item={"id": job_id, "owner": owner, "state": "uploading", "created": now,
                      "expires": now + 86400, "bytes": size, "sha256": checksum})
    url = boto3.client("s3", config=Config(signature_version="s3v4")).generate_presigned_url(
        "put_object", Params={"Bucket": os.environ["UPLOAD_BUCKET"], "Key": key,
                              "ContentType": "image/jpeg", "ContentLength": size,
                              "ChecksumSHA256": checksum, "IfNoneMatch": "*"}, ExpiresIn=120)
    return response(201, {"id": job_id, "uploadUrl": url})


def worker(event, _context):
    db = table()
    s3 = boto3.client("s3")
    for record in event["Records"]:
        key = unquote_plus(record["s3"]["object"]["key"])
        job_id = key.removeprefix("inputs/").removesuffix(".jpg")
        if key != "inputs/" + job_id + ".jpg" or len(job_id) != 32:
            continue
        try:
            job = db.update_item(Key={"id": job_id}, UpdateExpression="SET #state = :processing",
                ConditionExpression="#state = :uploading AND expires > :now",
                ExpressionAttributeNames={"#state": "state"},
                ExpressionAttributeValues={":processing": "processing", ":uploading": "uploading", ":now": int(time.time())},
                ReturnValues="ALL_NEW")["Attributes"]
        except ClientError as error:
            if error.response["Error"]["Code"] == "ConditionalCheckFailedException":
                continue  # Duplicate S3 notification or expired job.
            raise
        bench_key = "inputs/demo/" + job_id + ".jpg"
        result = {"success": False, "error": "ocr_failed"}
        try:
            obj = s3.get_object(Bucket=record["s3"]["bucket"]["name"], Key=key)
            with obj["Body"] as stream:
                image = stream.read(MAX_BYTES + 1)
            if len(image) != job["bytes"] or base64.b64encode(hashlib.sha256(image).digest()).decode() != job["sha256"]:
                raise ValueError("image_metadata_mismatch")
            with Image.open(io.BytesIO(image)) as decoded:
                if decoded.format != "JPEG" or not (0 < decoded.width <= 1200 and 0 < decoded.height <= 1200):
                    raise ValueError("invalid_image_dimensions")
                decoded.verify()
            s3.put_object(Bucket=os.environ["BENCH_BUCKET"], Key=bench_key, Body=image, ContentType="image/jpeg")
            invoked = boto3.client("lambda", config=Config(read_timeout=110, retries={"total_max_attempts": 1})).invoke(
                FunctionName=os.environ["BENCH_FUNCTION"], InvocationType="RequestResponse",
                Payload=json.dumps({"stitched": bench_key, "include_extraction": True}).encode())
            with invoked["Payload"] as payload:
                output = payload.read(256 * 1024)
            if "FunctionError" not in invoked:
                parsed = json.loads(output)
                if isinstance(parsed, dict) and type(parsed.get("success")) is bool:
                    result = parsed
        except (ValueError, UnidentifiedImageError):
            result = {"success": False, "error": "invalid_image_or_result"}
        except Exception:
            logging.exception("Demo OCR job failed job_id=%s", job_id)
        finally:
            try:
                s3.delete_object(Bucket=os.environ["BENCH_BUCKET"], Key=bench_key)
            except Exception:
                logging.warning("Demo input cleanup deferred to bucket lifecycle job_id=%s", job_id)
        db.update_item(Key={"id": job_id}, UpdateExpression="SET #state = :state, #result = :result",
            ExpressionAttributeNames={"#state": "state", "#result": "result"},
            ExpressionAttributeValues={":state": "complete" if result["success"] else "failed", ":result": json.dumps(result)})
