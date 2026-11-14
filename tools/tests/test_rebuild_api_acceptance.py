"""Offline safety checks; never loads local credentials or contacts Docker/API."""

import copy
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock
from urllib.error import URLError


MODULE = Path(__file__).with_name("rebuild_api_acceptance.py")
spec = importlib.util.spec_from_file_location("rebuild_api_acceptance", MODULE)
acceptance = importlib.util.module_from_spec(spec)
spec.loader.exec_module(acceptance)

IMAGE = "sha256:" + "a" * 64
SOURCE = "b" * 64
CONTAINER = "c" * 64


def valid_inputs():
    fixture = {
        "owner_marker": acceptance.OWNER,
        "project": "sub2api-rebuild-offline-fixture",
        "all_accounts_synthetic": True,
        "real_human_acknowledgement_performed": False,
        "api_base_url": "http://127.0.0.1:32180",
        "vendor_origin": "https://127.0.0.1:32182",
        "api_image_id": IMAGE,
        "api_source_worktree_sha256": SOURCE,
        "roles": {},
        "resources": [{"label": name, "account_id": i, "proxy_id": i}
                      for i, name in enumerate(("admin", "vendor_l1", "vendor_l2"), 1)],
        "content_fixtures": {"payments_performed": 0, "plan_for_sale": False,
                             "plan_price_is_synthetic": True},
    }
    for i, label in enumerate(acceptance.ROLE_NAMES, 4):
        role = ("vendor_level2" if label == "vendor_l2" else
                "vendor_level1" if label.startswith("vendor_") else
                "admin" if label == "admin_scoped" else "super_admin")
        fixture["roles"][label] = {
            "user_id": i, "desired_role": role, "email": label + "@example.invalid",
            "password": "offline-synthetic-value",
        }
    environment = {"project": fixture["project"], "contains_production_data": False,
                   "host": "127.0.0.1", "port": 32180, "api_container_id": CONTAINER,
                   "image_id": IMAGE, "source_worktree_sha256": SOURCE}
    container = {"Id": CONTAINER, "Image": IMAGE, "State": {"Running": True},
                 "Config": {"Labels": {"com.docker.compose.project": fixture["project"]}},
                 "NetworkSettings": {"Ports": {"8080/tcp": [
                     {"HostIp": "127.0.0.1", "HostPort": "32180"}]}}}
    return fixture, environment, container


class LocalAcceptanceSafetyTests(unittest.TestCase):
    def test_only_explicit_numeric_loopback_origin_is_allowed(self):
        self.assertEqual(acceptance.local_origin("http://127.0.0.1:32180/"),
                         "http://127.0.0.1:32180")
        for origin in ("https://prod.example:443", "http://localhost:32180",
                       "http://127.0.0.1", "http://127.0.0.1:32180/api",
                       "http://u:secret@127.0.0.1:32180", "http://127.0.0.1:32180?x=1",
                       "http://127.0.0.1:32180#fragment", "http://127.0.0.1:bad"):
            with self.subTest(origin=origin), self.assertRaises(acceptance.PreconditionError):
                acceptance.local_origin(origin)

    def test_vendor_origin_requires_https(self):
        with self.assertRaises(acceptance.PreconditionError):
            acceptance.local_origin("http://127.0.0.1:32182", https_only=True)

    def test_private_fixture_rejects_public_permissions_and_symlinks(self):
        with tempfile.TemporaryDirectory(prefix="sub2api-acceptance-unit-") as directory:
            path = Path(directory) / "fixture.json"
            path.write_text('{"synthetic":true}')
            path.chmod(0o600)
            self.assertEqual(acceptance.read_private_fixture(path), {"synthetic": True})
            path.chmod(0o644)
            with self.assertRaises(acceptance.PreconditionError):
                acceptance.read_private_fixture(path)
            path.chmod(0o600)
            link = Path(directory) / "link.json"
            link.symlink_to(path)
            with self.assertRaises(acceptance.PreconditionError):
                acceptance.read_private_fixture(link)

    def test_owned_frozen_fixture_and_container_match(self):
        fixture, environment, container = valid_inputs()
        base = acceptance.validate_fixture(fixture, environment, IMAGE, SOURCE)
        acceptance.verify_container(container, environment, base, IMAGE)

    def test_production_or_human_fixture_is_rejected_before_network(self):
        for key, value in (("all_accounts_synthetic", False),
                           ("real_human_acknowledgement_performed", True),
                           ("owner_marker", "unowned")):
            fixture, environment, _ = valid_inputs()
            fixture[key] = value
            with self.subTest(key=key), self.assertRaises(acceptance.PreconditionError):
                acceptance.validate_fixture(fixture, environment, IMAGE, SOURCE)
        fixture, environment, _ = valid_inputs()
        environment["contains_production_data"] = True
        with self.assertRaises(acceptance.PreconditionError):
            acceptance.validate_fixture(fixture, environment, IMAGE, SOURCE)

    def test_source_image_and_project_drift_are_rejected(self):
        for key in ("image_id", "source_worktree_sha256", "project", "api_container_id"):
            fixture, environment, _ = valid_inputs()
            environment[key] = "drifted"
            with self.subTest(key=key), self.assertRaises(acceptance.PreconditionError):
                acceptance.validate_fixture(fixture, environment, IMAGE, SOURCE)

    def test_roles_must_have_distinct_synthetic_identities(self):
        fixture, environment, _ = valid_inputs()
        for field, value in (("email", "person@example.com"), ("user_id", True),
                             ("password", ""), ("desired_role", "super_admin")):
            candidate = copy.deepcopy(fixture)
            candidate["roles"]["vendor_l1"][field] = value
            with self.subTest(field=field), self.assertRaises(acceptance.PreconditionError):
                acceptance.validate_fixture(candidate, environment, IMAGE, SOURCE)
        fixture["roles"]["vendor_l2"]["user_id"] = fixture["roles"]["vendor_l1"]["user_id"]
        with self.assertRaises(acceptance.PreconditionError):
            acceptance.validate_fixture(fixture, environment, IMAGE, SOURCE)

    def test_foreign_stopped_image_or_exposed_container_is_rejected(self):
        fixture, environment, container = valid_inputs()
        candidates = []
        candidate = copy.deepcopy(container)
        candidate["Config"]["Labels"]["com.docker.compose.project"] = "production"
        candidates.append(candidate)
        candidate = copy.deepcopy(container)
        candidate["State"]["Running"] = False
        candidates.append(candidate)
        candidate = copy.deepcopy(container)
        candidate["Image"] = "sha256:" + "d" * 64
        candidates.append(candidate)
        candidate = copy.deepcopy(container)
        candidate["NetworkSettings"]["Ports"]["8080/tcp"].append(
            {"HostIp": "0.0.0.0", "HostPort": "32180"})
        candidates.append(candidate)
        for candidate in candidates:
            with self.assertRaises(acceptance.PreconditionError):
                acceptance.verify_container(candidate, environment, fixture["api_base_url"], IMAGE)

    def test_mutating_or_encoded_routes_never_reach_transport(self):
        client = acceptance.API("http://127.0.0.1:32180")
        client.opener = Mock()
        for method, path in (
            ("POST", "/api/v1/admin/compliance/accept"),
            ("POST", "/api/v1/responses"), ("POST", "/api/v1/payments"),
            ("PATCH", "/api/v1/admin/settings"),
            ("GET", "/api/v1/settings/email-unsubscribe"),
            ("GET", "/api/v1/vendor/accounts/%2e%2e/refresh"),
            ("GET", "https://prod.example/api/v1/auth/me"),
        ):
            with self.subTest(method=method, path=path), self.assertRaises(acceptance.PreconditionError):
                client.request(method, path)
        client.opener.open.assert_not_called()

    def test_redirect_does_not_forward_credentials(self):
        redirect = acceptance.NoRedirect()
        with self.assertRaises(acceptance.PreconditionError):
            redirect.redirect_request(None, None, 307, "redirect", {}, "https://external.invalid")

    def test_transport_errors_do_not_echo_credentials(self):
        client = acceptance.API("http://127.0.0.1:32180")
        client.opener = Mock()
        client.opener.open.side_effect = URLError("fixture-secret-value")
        with self.assertRaises(AssertionError) as captured:
            client.request("GET", "/api/v1/auth/me", token="fixture-secret-value")
        self.assertNotIn("fixture-secret-value", str(captured.exception))

    def test_invalid_json_never_echoes_body(self):
        client = acceptance.API("http://127.0.0.1:32180")
        response = Mock()
        response.code = 200
        response.read.return_value = b"<html>fixture-secret-value</html>"
        response.__enter__ = Mock(return_value=response)
        response.__exit__ = Mock(return_value=False)
        client.opener = Mock()
        client.opener.open.return_value = response
        with self.assertRaises(AssertionError) as captured:
            client.request("GET", "/api/v1/auth/me")
        self.assertNotIn("fixture-secret-value", str(captured.exception))

    def test_invalid_header_errors_do_not_echo_tokens(self):
        client = acceptance.API("http://127.0.0.1:32180")
        client.opener = Mock()
        client.opener.open.side_effect = ValueError("Invalid header fixture-secret-value")
        with self.assertRaises(AssertionError) as captured:
            client.request("GET", "/api/v1/auth/me", token="fixture-secret-value")
        self.assertNotIn("fixture-secret-value", str(captured.exception))


if __name__ == "__main__":
    unittest.main()
