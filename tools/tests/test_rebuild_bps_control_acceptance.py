"""Offline guards for the explicitly invoked mutating BPS acceptance suite."""

from types import SimpleNamespace
import unittest
from unittest.mock import Mock

from tools.tests import rebuild_bps_control_acceptance as control


class BPSAcceptanceOwnershipTests(unittest.TestCase):
    def setUp(self):
        self.client = control.BPSControlAPI(
            "http://127.0.0.1:32180", SimpleNamespace(accounts=[1, 71]),
        )
        self.client.opener = Mock()

    def test_unowned_or_upstream_operations_never_reach_http(self):
        for method, path, payload in (
            ("PUT", "/api/v1/admin/accounts/72", {}),
            ("GET", "/api/v1/admin/accounts/72", None),
            ("PATCH", "/api/v1/vendor/accounts/72", {}),
            ("POST", "/api/v1/admin/accounts/71/refresh", {}),
            ("POST", "/api/v1/vendor/accounts/71/probe", {}),
            ("POST", "/api/v1/responses", {}),
            ("POST", "/api/v1/admin/accounts", {}),
            ("PUT", "/api/v1/admin/settings", {}),
            ("DELETE", "/api/v1/admin/accounts/71", {}),
        ):
            with self.subTest(method=method, path=path):
                with self.assertRaises(control.common.PreconditionError):
                    self.client.request(method, path, payload=payload)
        self.client.opener.open.assert_not_called()

    def test_bulk_cannot_expand_targets_or_treat_bool_as_account_id(self):
        for payload in (
            {}, {"account_ids": []}, {"account_ids": [71, 72]},
            {"account_ids": [True]}, {"account_ids": [71], "filters": {}},
            {"filters": {"status": "active"}},
        ):
            with self.subTest(payload=payload):
                with self.assertRaises(control.common.PreconditionError):
                    self.client.request("POST", "/api/v1/admin/accounts/bulk-update",
                                        payload=payload)
        self.client.opener.open.assert_not_called()

    def test_http_receipt_does_not_copy_tokens_or_account_payload(self):
        response = Mock()
        response.code = 200
        response.read.return_value = (
            b'{"code":0,"data":{"access_token":"private-fixture",'
            b'"credentials":{"access_token":"private-account"}}}'
        )
        response.__enter__ = Mock(return_value=response)
        response.__exit__ = Mock(return_value=False)
        self.client.opener.open.return_value = response
        self.client.request("POST", "/api/v1/auth/login",
                            payload={"email": "synthetic@example.invalid", "password": "private-password"})
        self.assertEqual(self.client.observations, [{
            "method": "POST", "path": "/api/v1/auth/login", "status": 200,
            "code": 0, "reason": None,
        }])


if __name__ == "__main__":
    unittest.main()
