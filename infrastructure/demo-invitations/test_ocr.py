import base64
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import unittest
from unittest.mock import Mock, patch

from botocore.exceptions import ClientError
from PIL import Image

spec = importlib.util.spec_from_file_location("demo_ocr", Path(__file__).parent / "ocr" / "ocr.py")
ocr = importlib.util.module_from_spec(spec)
spec.loader.exec_module(ocr)


def conditional_failure():
    return ClientError({"Error": {"Code": "ConditionalCheckFailedException"}}, "UpdateItem")


class DemoOcrTest(unittest.TestCase):
    def setUp(self):
        self.db = Mock()
        self.s3 = Mock()
        self.invoke = Mock()
        for patcher in [patch.object(ocr, "table", return_value=self.db),
                        patch.object(ocr.boto3, "client", side_effect=lambda name, **kw: self.s3 if name == "s3" else self.invoke),
                        patch.dict(os.environ, {"UPLOAD_BUCKET": "uploads", "BENCH_BUCKET": "bench", "BENCH_FUNCTION": "ocr"})]:
            patcher.start()
            self.addCleanup(patcher.stop)

    def event(self, method="GET", owner="alice"):
        return {"requestContext": {"http": {"method": method}, "authorizer": {"jwt": {"claims": {
            "sub": owner, "token_use": "access"}}}}, "pathParameters": {"id": "a" * 32},
            "body": json.dumps({"bytes": 100, "sha256": base64.b64encode(b"a" * 32).decode()})}

    def test_read_hides_other_users_and_expired_jobs(self):
        for owner, expires in [("bob", 9999999999), ("alice", 1)]:
            self.db.get_item.return_value = {"Item": {"owner": owner, "expires": expires}}
            self.assertEqual(404, ocr.api(self.event(), None)["statusCode"])

    def test_id_token_is_rejected_before_database_access(self):
        event = self.event()
        event["requestContext"]["authorizer"]["jwt"]["claims"]["token_use"] = "id"
        self.assertEqual(401, ocr.api(event, None)["statusCode"])
        self.db.get_item.assert_not_called()

    def test_upload_is_bounded_and_signed_once(self):
        self.s3.generate_presigned_url.return_value = "https://uploads.example/signed"
        self.assertEqual(201, ocr.api(self.event("POST"), None)["statusCode"])
        params = self.s3.generate_presigned_url.call_args.kwargs["Params"]
        self.assertEqual("*", params["IfNoneMatch"])
        self.assertEqual(100, params["ContentLength"])
        event = self.event("POST")
        event["body"] = json.dumps({"bytes": ocr.MAX_BYTES + 1, "sha256": params["ChecksumSHA256"]})
        self.assertEqual(400, ocr.api(event, None)["statusCode"])

    def test_rate_limit_returns_retry_message(self):
        self.db.put_item.side_effect = conditional_failure()
        self.assertEqual(429, ocr.api(self.event("POST"), None)["statusCode"])
        self.s3.generate_presigned_url.assert_not_called()

    def test_abandoned_job_reports_timeout(self):
        self.db.get_item.return_value = {"Item": {"owner": "alice", "expires": 9999999999,
                                                  "created": 1, "state": "processing"}}
        body = json.loads(ocr.api(self.event(), None)["body"])
        self.assertEqual("job_timed_out", body["error"])

    def run_worker(self, size=(100, 100)):
        output = io.BytesIO()
        Image.new("RGB", size).save(output, "JPEG")
        image = output.getvalue()
        self.db.update_item.return_value = {"Attributes": {
            "bytes": len(image), "sha256": base64.b64encode(hashlib.sha256(image).digest()).decode()}}
        self.s3.get_object.return_value = {"Body": io.BytesIO(image)}
        self.invoke.invoke.return_value = {"Payload": io.BytesIO(b'{"success":true,"extraction":{"name":"Test"}}')}
        ocr.worker({"Records": [{"s3": {"bucket": {"name": "uploads"},
                                         "object": {"key": "inputs/" + "a" * 32 + ".jpg"}}}]}, None)

    def test_worker_returns_extraction_and_cleans_temporary_copy(self):
        self.run_worker()
        self.invoke.invoke.assert_called_once()
        self.s3.delete_object.assert_called_once()
        values = self.db.update_item.call_args.kwargs["ExpressionAttributeValues"]
        self.assertEqual("complete", values[":state"])
        self.assertEqual("Test", json.loads(values[":result"])["extraction"]["name"])

    def test_large_dimensions_never_reach_model(self):
        self.run_worker((1201, 10))
        self.invoke.invoke.assert_not_called()
        values = self.db.update_item.call_args.kwargs["ExpressionAttributeValues"]
        self.assertEqual("failed", values[":state"])

    def test_duplicate_notification_never_reaches_model(self):
        self.db.update_item.side_effect = conditional_failure()
        self.run_worker()
        self.invoke.invoke.assert_not_called()
        self.s3.get_object.assert_not_called()
