"""Opt-in HTTP acceptance against the owned local rebuild Docker fixture.

This module deliberately does not match test_*.py. Run it explicitly after
source/image freeze; ordinary unittest discovery must never contact a server.
Only login and expected-denied export use POST. No setup, compliance acceptance,
payment, account refresh/probe, generation, or fixture mutations are performed.
"""

import argparse
from http.client import HTTPException
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import unittest
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import HTTPRedirectHandler, ProxyHandler, Request, build_opener


ROLE_NAMES = (
    "admin_synthetic", "admin_unconfirmed", "admin_scoped",
    "vendor_l1", "vendor_l2", "vendor_empty",
)
OWNER = "architecture-rebuild-main-role-fixtures-v1"
MAX_BODY_BYTES = 2 * 1024 * 1024


class PreconditionError(Exception):
    """Safe, credential-free prerequisite error."""


def local_origin(value, *, https_only=False):
    try:
        parts = urlsplit(value)
        port = parts.port
    except (TypeError, ValueError):
        raise PreconditionError("Invalid local origin") from None
    schemes = {"https"} if https_only else {"http", "https"}
    if (parts.scheme not in schemes or parts.hostname != "127.0.0.1"
            or not port or parts.username or parts.password
            or parts.path not in {"", "/"} or parts.query or parts.fragment):
        raise PreconditionError("Origin must be an explicit 127.0.0.1 port")
    return value.rstrip("/")


def read_private_fixture(path):
    """Read the existing private fixture without following a symlink."""
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
        with os.fdopen(fd, "rb") as stream:
            info = os.fstat(stream.fileno())
            if (not stat.S_ISREG(info.st_mode)
                    or stat.S_IMODE(info.st_mode) != 0o600
                    or info.st_uid != os.getuid()):
                raise PreconditionError("Fixture must be an owned regular 0600 file")
            raw = stream.read(MAX_BODY_BYTES + 1)
        if len(raw) > MAX_BODY_BYTES:
            raise PreconditionError("Fixture exceeds size limit")
        return json.loads(raw)
    except (OSError, ValueError):
        raise PreconditionError("Cannot read the private JSON fixture") from None


def positive_id(value):
    return type(value) is int and value > 0


def validate_fixture(fixture, environment, expected_image, expected_source):
    if not isinstance(fixture, dict) or not isinstance(environment, dict):
        raise PreconditionError("Fixture and environment must be objects")
    if (fixture.get("owner_marker") != OWNER
            or fixture.get("all_accounts_synthetic") is not True
            or fixture.get("real_human_acknowledgement_performed") is not False
            or environment.get("contains_production_data") is not False):
        raise PreconditionError("Only owned synthetic non-production fixtures are allowed")
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", expected_image):
        raise PreconditionError("Expected image must be a full Docker image ID")
    if not re.fullmatch(r"[0-9a-f]{64}", expected_source):
        raise PreconditionError("Expected source must be a SHA-256 fingerprint")
    project = fixture.get("project", "")
    if (not project.startswith("sub2api-rebuild-")
            or project != environment.get("project")):
        raise PreconditionError("Fixture does not belong to the rebuild Compose project")
    for obj, image_key, source_key in (
        (fixture, "api_image_id", "api_source_worktree_sha256"),
        (environment, "image_id", "source_worktree_sha256"),
    ):
        if obj.get(image_key) != expected_image or obj.get(source_key) != expected_source:
            raise PreconditionError("Fixture/image provenance differs from the frozen input")
    base = local_origin(fixture.get("api_base_url", ""))
    if (environment.get("host") != "127.0.0.1"
            or environment.get("port") != urlsplit(base).port):
        raise PreconditionError("Environment does not identify the fixture API binding")
    local_origin(fixture.get("vendor_origin", ""), https_only=True)
    roles = fixture.get("roles", {})
    if not isinstance(roles, dict) or set(roles) != set(ROLE_NAMES):
        raise PreconditionError("The six documented role fixtures are required")
    user_ids = set()
    for label in ROLE_NAMES:
        role = roles[label]
        expected_role = ("vendor_level2" if label == "vendor_l2" else
                         "vendor_level1" if label.startswith("vendor_") else
                         "admin" if label == "admin_scoped" else "super_admin")
        if (not isinstance(role, dict) or not positive_id(role.get("user_id"))
                or role["user_id"] in user_ids
                or role.get("desired_role") != expected_role
                or not isinstance(role.get("email"), str)
                or not role["email"].endswith("@example.invalid")
                or not isinstance(role.get("password"), str) or not role["password"]):
            raise PreconditionError("Role fixture identity is incomplete or not synthetic")
        user_ids.add(role["user_id"])
    resources = fixture.get("resources", [])
    if (not isinstance(resources, list) or len(resources) != 3
            or any(not isinstance(r, dict) for r in resources)
            or {r.get("label") for r in resources} != {"admin", "vendor_l1", "vendor_l2"}
            or any(not positive_id(r.get(k)) for r in resources
                   for k in ("account_id", "proxy_id"))):
        raise PreconditionError("Owned admin/L1/L2 resource fixtures are required")
    content = fixture.get("content_fixtures", {})
    if (content.get("payments_performed") != 0
            or content.get("plan_for_sale") is not False
            or content.get("plan_price_is_synthetic") is not True):
        raise PreconditionError("Content fixtures must remain synthetic and not for sale")
    container_id = environment.get("api_container_id", "")
    if not re.fullmatch(r"[0-9a-f]{64}", container_id):
        raise PreconditionError("An exact API container ID is required")
    return base


def verify_container(container, environment, base, expected_image):
    labels = container.get("Config", {}).get("Labels") or {}
    if (labels.get("com.docker.compose.project") != environment["project"]
            or container.get("Id") != environment["api_container_id"]
            or container.get("Image") != expected_image
            or container.get("State", {}).get("Running") is not True):
        raise PreconditionError("Running container identity/image/project does not match")
    port = str(urlsplit(base).port)
    ports = container.get("NetworkSettings", {}).get("Ports") or {}
    bindings = [b for group in ports.values() if group for b in group]
    if not any(b.get("HostIp") == "127.0.0.1" and b.get("HostPort") == port
               for b in bindings):
        raise PreconditionError("Expected API port is not bound on loopback by this container")
    if any(b.get("HostPort") == port and b.get("HostIp") != "127.0.0.1"
           for b in bindings):
        raise PreconditionError("API acceptance port is also exposed outside loopback")


class NoRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        raise PreconditionError("Acceptance HTTP redirects are forbidden")


class API:
    def __init__(self, base):
        self.base = local_origin(base)
        # Ignore process proxy variables; never send fixture secrets to a proxy.
        self.opener = build_opener(ProxyHandler({}), NoRedirect())

    def validate_request(self, method, path, payload):
        read_path = re.fullmatch(
            r"/api/v1/(auth/me|admin/(settings|groups|compliance)|settings/public|"
            r"vendor/(me|accounts|proxies)(/[1-9][0-9]*)?)", path,
        )
        if method == "GET" and not read_path:
            raise PreconditionError("Invalid acceptance API path")
        if method not in {"GET", "POST"} or (method == "POST" and path not in {
            "/api/v1/auth/login", "/api/v1/vendor/accounts/export",
        }):
            raise PreconditionError("Mutation is outside this read/denial suite")

    def request(self, method, path, *, token=None, origin=None, payload=None):
        self.validate_request(method, path, payload)
        headers = {"Accept": "application/json"}
        if token:
            headers["Authorization"] = "Bearer " + token
        if origin:
            headers["Origin"] = local_origin(origin, https_only=True)
        body = None
        if payload is not None:
            body = json.dumps(payload).encode()
            headers["Content-Type"] = "application/json"
        req = Request(self.base + path, data=body, headers=headers, method=method)
        try:
            response = self.opener.open(req, timeout=15)
        except HTTPError as error:
            response = error
        except (URLError, OSError, ValueError, HTTPException):
            raise AssertionError("Local acceptance HTTP transport failed") from None
        with response:
            status_code = response.code
            raw = response.read(MAX_BODY_BYTES + 1)
        if len(raw) > MAX_BODY_BYTES:
            raise AssertionError("Acceptance response exceeds size limit")
        try:
            data = json.loads(raw)
        except (ValueError, UnicodeDecodeError):
            raise AssertionError("Expected the real JSON API, not HTML or a proxy page") from None
        if not isinstance(data, dict):
            raise AssertionError("API envelope must be an object")
        return status_code, data


class RebuildAPIAcceptance(unittest.TestCase):
    fixture = None
    api = None

    @classmethod
    def setUpClass(cls):
        if cls.fixture is None or cls.api is None:
            raise PreconditionError("Run this suite via its explicit CLI after freeze")
        cls.tokens = {}
        cls.login_users = {}
        for label in ROLE_NAMES:
            role = cls.fixture["roles"][label]
            status_code, envelope = cls.api.request(
                "POST", "/api/v1/auth/login", origin=cls.origin(label),
                payload={"email": role["email"], "password": role["password"]},
            )
            data = envelope.get("data")
            if status_code != 200 or envelope.get("code") != 0 or not isinstance(data, dict):
                raise AssertionError("Synthetic role login failed: " + label)
            if not isinstance(data.get("access_token"), str) or not data["access_token"]:
                raise AssertionError("Login omitted the access token: " + label)
            cls.tokens[label] = data["access_token"]
            cls.login_users[label] = data.get("user", {})

    @classmethod
    def origin(cls, label):
        return cls.fixture["vendor_origin"] if label and label.startswith("vendor_") else None

    def call(self, path, label=None, *, method="GET", payload=None):
        return self.api.request(method, path, token=self.tokens.get(label),
                                origin=self.origin(label), payload=payload)

    def ok(self, path, label=None):
        status_code, envelope = self.call(path, label)
        self.assertEqual(status_code, 200, "Expected HTTP 200 for " + path)
        self.assertTrue(envelope.get("code") == 0, "Expected a success API envelope")
        data = envelope.get("data")
        self.assertTrue(isinstance(data, dict), "Expected object data")
        return data

    def resource(self, label, key):
        return next(r[key] for r in self.fixture["resources"] if r["label"] == label)

    def test_01_anonymous_management_requires_login(self):
        for path in ("/api/v1/auth/me", "/api/v1/admin/settings", "/api/v1/vendor/me"):
            with self.subTest(path=path):
                self.assertEqual(self.call(path)[0], 401)

    def test_02_malformed_bearer_is_rejected(self):
        status_code, _ = self.api.request("GET", "/api/v1/auth/me", token="synthetic.invalid.token")
        self.assertEqual(status_code, 401)

    def test_03_login_and_me_keep_exact_role_identity(self):
        for label in ROLE_NAMES:
            with self.subTest(role=label):
                expected = self.fixture["roles"][label]
                for user in (self.login_users[label], self.ok("/api/v1/auth/me", label)):
                    self.assertTrue(user.get("id") == expected["user_id"], "Unexpected user identity")
                    self.assertTrue(user.get("role") == expected["desired_role"], "Unexpected role")

    def test_04_synthetic_admin_can_read_groups(self):
        state = self.ok("/api/v1/admin/compliance", "admin_synthetic")
        self.assertIs(state.get("required"), False)
        self.ok("/api/v1/admin/groups", "admin_synthetic")

    def test_05_unconfirmed_admin_stays_unconfirmed(self):
        before = self.ok("/api/v1/admin/compliance", "admin_unconfirmed")
        self.assertIs(before.get("required"), True)
        status_code, envelope = self.call("/api/v1/admin/groups", "admin_unconfirmed")
        self.assertEqual(status_code, 423)
        self.assertTrue(envelope.get("code") == "ADMIN_COMPLIANCE_ACK_REQUIRED")
        self.assertIs(self.ok("/api/v1/admin/compliance", "admin_unconfirmed").get("required"), True)

    def test_06_scoped_admin_cannot_read_groups(self):
        status_code, envelope = self.call("/api/v1/admin/groups", "admin_scoped")
        self.assertEqual(status_code, 403)
        self.assertTrue(envelope.get("code") == "ADMIN_PERMISSION_DENIED")

    def test_07_vendors_cannot_read_admin_settings(self):
        for label in ("vendor_l1", "vendor_l2", "vendor_empty"):
            with self.subTest(role=label):
                self.assertEqual(self.call("/api/v1/admin/settings", label)[0], 403)

    def test_08_vendor_identity_matches_logged_in_user(self):
        for label in ("vendor_l1", "vendor_l2", "vendor_empty"):
            with self.subTest(role=label):
                data = self.ok("/api/v1/vendor/me", label)
                self.assertTrue(data.get("user_id") == self.fixture["roles"][label]["user_id"])

    def test_09_empty_vendor_is_not_global_scope(self):
        for resource in ("accounts", "proxies"):
            with self.subTest(resource=resource):
                data = self.ok("/api/v1/vendor/" + resource, "vendor_empty")
                self.assertTrue(data.get("total") == 0, "Empty scope must have total zero")
                self.assertTrue(data.get("items") in (None, []), "Empty scope leaked objects")

    def test_10_empty_vendor_cannot_read_foreign_objects(self):
        for label in ("vendor_l1", "vendor_l2"):
            for kind, key in (("accounts", "account_id"), ("proxies", "proxy_id")):
                with self.subTest(owner=label, resource=kind):
                    path = "/api/v1/vendor/{}/{}".format(kind, self.resource(label, key))
                    self.assertEqual(self.call(path, "vendor_empty")[0], 404)

    def test_11_vendor_owned_objects_are_readable(self):
        for label in ("vendor_l1", "vendor_l2"):
            for kind, key in (("accounts", "account_id"), ("proxies", "proxy_id")):
                with self.subTest(role=label, resource=kind):
                    object_id = self.resource(label, key)
                    data = self.ok("/api/v1/vendor/{}/{}".format(kind, object_id), label)
                    self.assertTrue(data.get("id") == object_id, "Wrong resource returned")

    def test_12_sensitive_export_requires_step_up(self):
        for label in ("vendor_l1", "vendor_l2"):
            with self.subTest(role=label):
                status_code, envelope = self.call(
                    "/api/v1/vendor/accounts/export", label, method="POST",
                    payload={"ids": [self.resource(label, "account_id")]},
                )
                self.assertEqual(status_code, 403)
                self.assertTrue(envelope.get("code") == "STEP_UP_TOTP_NOT_ENABLED")

    def test_13_public_settings_exclude_credential_fields(self):
        data = self.ok("/api/v1/settings/public")
        forbidden = {"password", "smtp_password", "client_secret", "access_token",
                     "refresh_token", "private_key", "jwt_secret", "database_url"}
        def check(value):
            if isinstance(value, dict):
                self.assertFalse(set(value) & forbidden, "Public configuration contains secret fields")
                for child in value.values():
                    check(child)
            elif isinstance(value, list):
                for child in value:
                    check(child)
        check(data)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixtures", type=Path, required=True)
    parser.add_argument("--environment", type=Path, required=True)
    parser.add_argument("--expected-image-id", required=True)
    parser.add_argument("--expected-source-sha256", required=True)
    args = parser.parse_args(argv)
    try:
        fixture = read_private_fixture(args.fixtures)
        environment = json.loads(args.environment.read_text())
        base = validate_fixture(fixture, environment, args.expected_image_id, args.expected_source_sha256)
        result = subprocess.run(
            ["docker", "inspect", "--type", "container", environment["api_container_id"]],
            check=True, capture_output=True, text=True, timeout=15,
        )
        containers = json.loads(result.stdout)
        if not isinstance(containers, list) or len(containers) != 1:
            raise PreconditionError("Expected exactly one owned API container")
        verify_container(containers[0], environment, base, args.expected_image_id)
    except PreconditionError as error:
        print("BLOCKED: " + str(error), file=sys.stderr)
        return 2
    except (OSError, ValueError, TypeError, KeyError, subprocess.SubprocessError):
        print("BLOCKED: cannot validate local fixture/container prerequisites", file=sys.stderr)
        return 2
    RebuildAPIAcceptance.fixture = fixture
    RebuildAPIAcceptance.api = API(base)
    print("Local Docker API subset; image=" + args.expected_image_id
          + " source=" + args.expected_source_sha256)
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(RebuildAPIAcceptance)
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    return 0 if result.wasSuccessful() and result.testsRun > 0 and not result.skipped else 1


if __name__ == "__main__":
    sys.exit(main())
