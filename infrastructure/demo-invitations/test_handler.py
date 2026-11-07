import importlib.util
import json
import os
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

spec = importlib.util.spec_from_file_location("handler", Path(__file__).parent / "site" / "handler.py")
handler = importlib.util.module_from_spec(spec)
spec.loader.exec_module(handler)


class InvitationsTest(unittest.TestCase):
    def setUp(self):
        settings = patch.dict(os.environ, {
            "DEMO_LOGIN": "https://demo.auth.us-east-2.amazoncognito.com",
            "DEMO_POOL_ID": "demo-pool",
        })
        settings.start()
        self.addCleanup(settings.stop)

    def request(self, claims, email="Tester@example.com"):
        return handler.handle({
            "rawPath": "/api/invitations",
            "requestContext": {"http": {"method": "POST"}, "authorizer": {"jwt": {"claims": claims}}},
            "body": json.dumps({"email": email}),
        }, None)

    def test_only_administrator_access_tokens_can_invite(self):
        for claims in ({}, {"token_use": "access", "cognito:groups": ["tester"]},
                       {"token_use": "id", "cognito:groups": ["invitation-admins"]}):
            self.assertEqual(403, self.request(claims)["statusCode"])

    def test_invalid_email_never_calls_cognito(self):
        self.assertEqual(400, self.request({"token_use": "access", "cognito:groups": "invitation-admins"},
                                           "invalid")["statusCode"])

    def test_admin_requests_cognito_email_without_returning_credentials(self):
        client = Mock()
        sdk = SimpleNamespace(client=Mock(return_value=client))
        with patch.dict(sys.modules, {"boto3": sdk, "botocore.exceptions": SimpleNamespace(ClientError=Exception)}):
            result = self.request({"token_use": "access", "cognito:groups": '["invitation-admins"]'})
        client.admin_create_user.assert_called_once_with(
            UserPoolId="demo-pool", Username="tester@example.com",
            UserAttributes=[{"Name": "email", "Value": "tester@example.com"}], DesiredDeliveryMediums=["EMAIL"],
        )
        self.assertEqual(201, result["statusCode"])
        self.assertEqual({"status": "invitation_requested"}, json.loads(result["body"]))
        self.assertEqual("no-store", result["headers"]["cache-control"])
