"""Explicit local Docker BPS control acceptance, using temporary owned records.

Unlike rebuild_api_acceptance, this suite writes only accounts created by this
run. PostgreSQL seeds a system-disabled state; the behavior under test goes
through the real HTTP handlers and transactions. No recovery probe, generation,
external authorization or production access is performed.
"""

import argparse
from concurrent.futures import ThreadPoolExecutor
from datetime import datetime, timezone
import json
from pathlib import Path
import re
import subprocess
import sys
import threading
import time
import unittest
import uuid

try:
    from . import rebuild_api_acceptance as common
except ImportError:
    import rebuild_api_acceptance as common


ENABLED = "openai_excel_bps"
MODELS = "openai_excel_bps_models"
DISABLED_AT = "openai_excel_bps_403_disabled_at"
PROBE_AT = "openai_excel_bps_403_last_probe_at"
FAILURE = "openai_excel_bps_failure"
EXPECTED = "openai_excel_bps_expected_disabled_at"
CONFLICT = "OPENAI_EXCEL_BPS_STATE_CONFLICT"


def sql_text(value):
    return "'" + value.replace("'", "''") + "'"


class OwnedPostgres:
    def __init__(self, fixture):
        self.marker = "rebuild-bps-control-" + uuid.uuid4().hex[:16]
        self.accounts = []
        self.user_id = None
        self.vendor_id = None
        self.fixture = fixture
        ids = subprocess.check_output([
            "docker", "ps", "-q",
            "--filter", "label=com.docker.compose.project=" + fixture["project"],
            "--filter", "label=com.docker.compose.service=postgres",
        ], text=True, timeout=15).split()
        if len(ids) != 1:
            raise common.PreconditionError("One owned local PostgreSQL container required")
        container = json.loads(subprocess.check_output(
            ["docker", "inspect", ids[0]], text=True, timeout=15,
        ))[0]
        labels = container["Config"].get("Labels") or {}
        if (labels.get("com.docker.compose.project") != fixture["project"]
                or labels.get("com.docker.compose.service") != "postgres"
                or container.get("State", {}).get("Running") is not True
                or any(container["NetworkSettings"].get("Ports", {}).values())):
            raise common.PreconditionError("PostgreSQL must be the unexposed test container")
        self.container_id = container["Id"]
        env = dict(pair.split("=", 1) for pair in container["Config"]["Env"] if "=" in pair)
        self.db_user = env.get("POSTGRES_USER", "postgres")
        self.db_name = env.get("POSTGRES_DB", self.db_user)

    def execute(self, statement):
        result = subprocess.run([
            "docker", "exec", "-i", self.container_id, "psql", "-X", "-A", "-t", "-q",
            "-v", "ON_ERROR_STOP=1", "-U", self.db_user, "-d", self.db_name,
        ], input=statement, capture_output=True, text=True, timeout=20)
        if result.returncode:
            # SQL/row error details can contain credentials; do not print them.
            raise common.PreconditionError("Owned PostgreSQL fixture operation failed")
        lines = result.stdout.strip().splitlines()
        return json.loads(lines[-1]) if lines else None

    def create_vendor(self):
        role = self.fixture["roles"]["vendor_l1"]
        self.email = self.marker + "@example.invalid"
        self.user_id = self.execute(
            "INSERT INTO users(email,password_hash,role,status,notes) "
            "SELECT " + sql_text(self.email) + ",password_hash,'vendor_level1','active',"
            + sql_text(self.marker) + " FROM users WHERE id=" + str(role["user_id"])
            + " AND email=" + sql_text(role["email"])
            + " AND deleted_at IS NULL RETURNING id;"
        )
        if not common.positive_id(self.user_id):
            raise common.PreconditionError("Synthetic source user did not match")
        self.vendor_id = self.execute(
            "INSERT INTO vendors(user_id,name,platforms,openai_authorization_modes) VALUES("
            + str(self.user_id) + "," + sql_text(self.marker)
            + ",'[\"openai\"]','[\"native\",\"bps\"]') RETURNING id;"
        )
        return {"email": self.email, "password": role["password"]}

    def create_account(self, vendor=False):
        n = len(self.accounts) + 1
        stamp = "2026-10-27T01:02:03." + str(n).zfill(6) + "Z"
        credentials = {
            "access_token": self.marker + "-invalid-" + str(n),
            "plan_type": "plus",
            "expires_at": "2099-01-01T00:00:00Z",
            "chatgpt_account_id": self.marker + "-" + str(n),
        }
        extra = {
            ENABLED: False, MODELS: ["gpt-5.1"], DISABLED_AT: stamp, PROBE_AT: stamp,
            "openai_excel_bps_auto_disable_on_403": True,
            "openai_excel_bps_auto_recover_on_403": False,
            FAILURE: {"source": "system", "state": "disabled", "reason": "account_forbidden",
                      "occurred_at": stamp, "grant_fingerprint": self.marker + "-private",
                      "policy": {"private_fixture": True}},
        }
        account_id = self.execute(
            "INSERT INTO accounts(name,platform,type,credentials,extra,status,schedulable,"
            "notes,vendor_id) VALUES("
            + sql_text(self.marker + "-" + str(n)) + ",'openai','oauth',"
            + sql_text(json.dumps(credentials)) + "::jsonb,"
            + sql_text(json.dumps(extra)) + "::jsonb,'inactive',false,"
            + sql_text(self.marker) + "," + (str(self.vendor_id) if vendor else "NULL")
            + ") RETURNING id;"
        )
        if not common.positive_id(account_id):
            raise common.PreconditionError("Owned account seed failed")
        self.accounts.append(account_id)
        return account_id, stamp

    def state(self, account_id):
        if account_id not in self.accounts:
            raise common.PreconditionError("Read outside owned acceptance records")
        result = self.execute(
            "SELECT json_build_object('name',name,'credentials',credentials,'extra',extra,"
            "'status',status,'schedulable',schedulable,'vendor_notes',vendor_notes,"
            "'outbox',(SELECT count(*) FROM scheduler_outbox WHERE account_id=accounts.id)) "
            "FROM accounts WHERE id=" + str(account_id) + " AND notes=" + sql_text(self.marker)
            + " AND deleted_at IS NULL;"
        )
        if not isinstance(result, dict):
            raise common.PreconditionError("Owned account provenance lost")
        return result

    def cleanup(self):
        if self.accounts:
            ids = ",".join(str(value) for value in self.accounts)
            owned = self.execute(
                "SELECT count(*) FROM accounts WHERE id IN (" + ids + ") AND notes="
                + sql_text(self.marker) + ";"
            )
            if owned != len(self.accounts):
                raise common.PreconditionError("Cleanup refused: account ownership changed")
            self.execute(
                "BEGIN; DELETE FROM scheduler_outbox WHERE account_id IN (" + ids + ");"
                "DELETE FROM accounts WHERE id IN (" + ids + ") AND notes="
                + sql_text(self.marker) + "; COMMIT;"
            )
            if self.execute("SELECT count(*) FROM accounts WHERE id IN (" + ids + ");") != 0:
                raise common.PreconditionError("Owned account cleanup incomplete")
        if self.vendor_id:
            self.execute("DELETE FROM vendors WHERE id=" + str(self.vendor_id)
                         + " AND name=" + sql_text(self.marker) + ";")
            if self.execute("SELECT count(*) FROM vendors WHERE id=" + str(self.vendor_id) + ";"):
                raise common.PreconditionError("Owned vendor cleanup incomplete")
        if self.user_id:
            self.execute("DELETE FROM users WHERE id=" + str(self.user_id)
                         + " AND notes=" + sql_text(self.marker) + ";")
            if self.execute("SELECT count(*) FROM users WHERE id=" + str(self.user_id) + ";"):
                raise common.PreconditionError("Owned user cleanup incomplete")
        return {"account_count_removed": len(self.accounts), "vendor_removed": bool(self.vendor_id),
                "user_removed": bool(self.user_id)}


class BPSControlAPI(common.API):
    def __init__(self, base, records):
        super().__init__(base)
        self.records = records
        self.observations = []

    def validate_request(self, method, path, payload):
        if method == "POST" and path == "/api/v1/auth/login":
            return
        if method == "GET" and path == "/api/v1/vendor/accounts":
            return
        if method == "POST" and path == "/api/v1/admin/accounts/bulk-update":
            targets = payload.get("account_ids", []) if isinstance(payload, dict) else []
            if (targets and all(type(value) is int and value in self.records.accounts
                                for value in targets) and "filters" not in payload):
                return
        match = re.fullmatch(
            r"/api/v1/(admin|vendor)/accounts/([1-9][0-9]*)(/apply-oauth-credentials)?", path,
        )
        if match and int(match[2]) in self.records.accounts:
            allowed = (method == "GET" and not match[3]
                       or method == "PUT" and match[1] == "admin" and not match[3]
                       or method == "PATCH" and match[1] == "vendor" and not match[3]
                       or method == "POST" and match[1] == "admin" and bool(match[3]))
            if allowed:
                return
        raise common.PreconditionError("Only this run's BPS control records may be changed")

    def request(self, method, path, **kwargs):
        status, envelope = super().request(method, path, **kwargs)
        self.observations.append({"method": method, "path": path, "status": status,
                                  "code": envelope.get("code"), "reason": envelope.get("reason")})
        return status, envelope


class BPSControlAcceptance(unittest.TestCase):
    api = None
    records = None
    fixture = None

    @classmethod
    def setUpClass(cls):
        if cls.api is None:
            raise common.PreconditionError("Run the explicit Docker acceptance CLI")
        vendor = cls.records.create_vendor()
        cls.tokens = {}
        for label, identity in (("admin", cls.fixture["roles"]["admin_synthetic"]),
                                ("vendor", vendor),
                                ("foreign", cls.fixture["roles"]["vendor_empty"])):
            status, body = cls.api.request(
                "POST", "/api/v1/auth/login", origin=cls.origin(label),
                payload={"email": identity["email"], "password": identity["password"]},
            )
            if status != 200 or not body.get("data", {}).get("access_token"):
                raise common.PreconditionError("Synthetic login failed: " + label)
            cls.tokens[label] = body["data"]["access_token"]

    @classmethod
    def origin(cls, label):
        return None if label == "admin" else cls.fixture["vendor_origin"]

    def call(self, account_id, payload=None, *, actor="admin", action="edit"):
        family = "admin" if actor == "admin" else "vendor"
        path = "/api/v1/" + family + "/accounts/" + str(account_id)
        method = "PUT" if actor == "admin" else "PATCH"
        if action == "read":
            method = "GET"
        elif action == "oauth":
            path += "/apply-oauth-credentials"
            method = "POST"
        elif action == "bulk":
            path, method = "/api/v1/admin/accounts/bulk-update", "POST"
            payload = dict(payload, account_ids=[account_id])
        return self.api.request(method, path, payload=payload, token=self.tokens[actor],
                                origin=self.origin(actor))

    def assert_success(self, result):
        status, body = result
        self.assertEqual(status, 200, "HTTP status; API code=" + str(body.get("code")))
        self.assertEqual(body.get("code"), 0)
        return body["data"]

    def assert_conflict(self, result):
        status, body = result
        self.assertEqual(status, 409, "HTTP status; API code=" + str(body.get("code")))
        self.assertEqual(body.get("code"), 409)
        self.assertEqual(body.get("reason"), CONFLICT)

    def assert_safe(self, data):
        serialized = json.dumps(data)
        for key in (FAILURE, "grant_fingerprint", "private_fixture", EXPECTED):
            self.assertNotIn(key, serialized, "Private recovery data in public DTO")

    def test_01_admin_safe_projection_and_stale_edit_rollback(self):
        account, stamp = self.records.create_account()
        data = self.assert_success(self.call(account, action="read"))
        self.assertEqual(data["excel_bps_403_recovery"]["disabled_at"], stamp)
        self.assert_safe(data)
        before = self.records.state(account)
        self.assert_conflict(self.call(account, {
            "name": self.records.marker + "-stale", "extra": {ENABLED: True},
        }))
        self.assertTrue(self.records.state(account) == before,
                        "Conflict must roll back metadata, recovery state and outbox")

    def test_02_admin_unrelated_edit_keeps_recovery_authority(self):
        account, stamp = self.records.create_account()
        self.assert_success(self.call(account, {"name": self.records.marker + "-metadata"}))
        state = self.records.state(account)
        self.assertIs(state["extra"][ENABLED], False)
        self.assertEqual(state["extra"][DISABLED_AT], stamp)
        self.assertEqual(state["extra"][PROBE_AT], stamp)
        self.assertIn(FAILURE, state["extra"])

    def test_03_admin_conditional_reenable_consumes_condition(self):
        account, stamp = self.records.create_account()
        self.assert_conflict(self.call(account, {"extra": {ENABLED: True, EXPECTED: stamp[:-2] + "9Z"}}))
        self.assert_success(self.call(account, {"extra": {ENABLED: True, EXPECTED: stamp}}))
        state = self.records.state(account)
        self.assertIs(state["extra"][ENABLED], True)
        for key in (EXPECTED, DISABLED_AT, PROBE_AT, FAILURE):
            self.assertNotIn(key, state["extra"])
        self.assertFalse(state["schedulable"], "Fixture must never become schedulable")
        self.assert_conflict(self.call(account, {"extra": {ENABLED: True, EXPECTED: stamp}}))

    def test_04_admin_false_cancels_already_disabled_claim(self):
        account, _ = self.records.create_account()
        self.assert_success(self.call(account, {"extra": {ENABLED: False}}))
        extra = self.records.state(account)["extra"]
        self.assertIs(extra[ENABLED], False)
        for key in (EXPECTED, DISABLED_AT, PROBE_AT, FAILURE):
            self.assertNotIn(key, extra)
        # The admin full-edit contract clears BPS options on an explicit stop.
        # Supplier PATCH has different replacement semantics; test it separately.
        self.assertIsNone(extra[MODELS])

    def test_05_bulk_conflict_rolls_back_then_matching_condition_works(self):
        account, stamp = self.records.create_account()
        before = self.records.state(account)
        self.assert_conflict(self.call(account, {"name": self.records.marker + "-bulk",
                                               "extra": {ENABLED: True}}, action="bulk"))
        self.assertTrue(self.records.state(account) == before, "Bulk conflict changed storage")
        data = self.assert_success(self.call(
            account, {"extra": {ENABLED: True, EXPECTED: stamp}}, action="bulk",
        ))
        self.assertEqual(data["success"], 1)
        self.assertEqual(data["failed"], 0)
        self.assertNotIn(EXPECTED, self.records.state(account)["extra"])

    def test_06_oauth_identity_guard_and_atomic_extra_conflict(self):
        account, stamp = self.records.create_account()
        before = self.records.state(account)
        replacement = self.records.marker + "-replacement-invalid"
        # Authentic replacement needs provider verification. Invalid synthetic
        # credentials must be refused, rather than weakening that production guard.
        status, body = self.call(
            account, {"type": "oauth", "credentials": {"access_token": replacement},
                      "extra": {ENABLED: True}}, action="oauth",
        )
        self.assertEqual(status, 422)
        self.assertEqual(body.get("reason"), "VENDOR_IDENTITY_UNVERIFIED")
        self.assertTrue(self.records.state(account) == before,
                        "Unverified OAuth input must not change credentials or extra")
        # Reapply the unchanged owned credentials to reach the real transactional
        # Extra path. This proves HTTP conflict propagation, not provider auth.
        self.assert_conflict(self.call(
            account, {"type": "oauth", "credentials": before["credentials"],
                      "extra": {ENABLED: True}}, action="oauth",
        ))
        self.assertTrue(self.records.state(account) == before,
                        "OAuth Extra conflict must roll back the whole request")
        self.assert_success(self.call(
            account, {"type": "oauth", "credentials": before["credentials"],
                      "extra": {ENABLED: True, EXPECTED: stamp}}, action="oauth",
        ))
        after = self.records.state(account)
        self.assertTrue(after["credentials"] == before["credentials"])
        self.assertIs(after["extra"][ENABLED], True)
        self.assertFalse(after["schedulable"])

    def test_07_vendor_list_detail_projection_and_foreign_denial(self):
        account, stamp = self.records.create_account(vendor=True)
        detail = self.assert_success(self.call(account, actor="vendor", action="read"))
        self.assert_safe(detail)
        self.assertEqual(detail["excel_bps_403_recovery"]["disabled_at"], stamp)
        result = self.api.request("GET", "/api/v1/vendor/accounts",
                                  token=self.tokens["vendor"], origin=self.origin("vendor"))
        data = self.assert_success(result)
        item = next(value for value in data["items"] if value["id"] == account)
        self.assertEqual(item["excel_bps_403_recovery"], detail["excel_bps_403_recovery"])
        self.assert_safe(item)
        self.assertIn(self.call(account, actor="foreign", action="read")[0], (403, 404))

    def test_08_vendor_stale_edit_and_retired_all_models_are_rejected(self):
        account, _ = self.records.create_account(vendor=True)
        before = self.records.state(account)
        self.assert_conflict(self.call(
            account, {"name": self.records.marker + "-stale", "bps_enabled": True},
            actor="vendor",
        ))
        self.assertTrue(self.records.state(account) == before, "Supplier metadata did not roll back")
        for value in (True, False):
            with self.subTest(bps_all_models=value):
                status, body = self.call(account, {"bps_all_models": value}, actor="vendor")
                self.assertEqual(status, 422)
                self.assertEqual(body.get("reason"), "VENDOR_ACCOUNT_INPUT")
                self.assertTrue(self.records.state(account) == before,
                                "Retired all-models input changed selection or recovery")

    def test_09_vendor_models_edit_preserves_claim_and_conditional_enable(self):
        account, stamp = self.records.create_account(vendor=True)
        self.assert_success(self.call(account, {"bps_models": ["gpt-5.1", "gpt-5.2"]},
                                      actor="vendor"))
        extra = self.records.state(account)["extra"]
        self.assertEqual(extra[PROBE_AT], stamp)
        self.assertIs(extra[ENABLED], False)
        self.assert_conflict(self.call(account, {"bps_enabled": True}, actor="vendor"))
        self.assert_success(self.call(account, {"bps_enabled": True, "bps_expected_disabled_at": stamp},
                                      actor="vendor"))
        extra = self.records.state(account)["extra"]
        self.assertEqual(extra[MODELS], ["gpt-5.1", "gpt-5.2"])
        self.assertIs(extra[ENABLED], True)
        self.assertNotIn(EXPECTED, extra)
        self.assertNotIn(FAILURE, extra)

    def test_10_vendor_explicit_false_and_foreign_write_denial(self):
        account, _ = self.records.create_account(vendor=True)
        before = self.records.state(account)
        self.assertIn(self.call(account, {"bps_enabled": False}, actor="foreign")[0], (403, 404))
        self.assertTrue(self.records.state(account) == before)
        self.assert_success(self.call(account, {"bps_enabled": False}, actor="vendor"))
        extra = self.records.state(account)["extra"]
        self.assertIs(extra[ENABLED], False)
        for key in (DISABLED_AT, PROBE_AT, FAILURE):
            self.assertNotIn(key, extra)


class BPSReplicaAcceptance(BPSControlAcceptance):
    replica = None

    @classmethod
    def setUpClass(cls):
        # The first suite owns setup/cleanup. Reuse its isolated identities;
        # do not create another supplier or rerun the ten single-instance cases.
        cls.tokens = BPSControlAcceptance.tokens

    def replica_call(self, account, payload=None, *, actor="admin", action="edit"):
        family = "admin" if actor == "admin" else "vendor"
        method = "GET" if action == "read" else "PUT" if actor == "admin" else "PATCH"
        return self.replica.request(
            method, "/api/v1/" + family + "/accounts/" + str(account), payload=payload,
            token=self.tokens[actor], origin=self.origin(actor),
        )

    def concurrent_enables(self, vendor):
        account, stamp = self.records.create_account(vendor=vendor)
        barrier = threading.Barrier(3)
        actor = "vendor" if vendor else "admin"
        payload = ({"bps_enabled": True, "bps_expected_disabled_at": stamp} if vendor
                   else {"extra": {ENABLED: True, EXPECTED: stamp}})
        names = [self.records.marker + "-writer-a", self.records.marker + "-writer-b"]
        def write(index, client):
            barrier.wait(timeout=5)
            return client(account, dict(payload, name=names[index]), actor=actor)
        with ThreadPoolExecutor(max_workers=2) as pool:
            a = pool.submit(write, 0, self.call)
            b = pool.submit(write, 1, self.replica_call)
            barrier.wait(timeout=5)
            results = [a.result(timeout=20), b.result(timeout=20)]
        self.assertEqual(sorted(status for status, _ in results), [200, 409])
        winner = next(i for i, result in enumerate(results) if result[0] == 200)
        self.assert_success(results[winner])
        self.assert_conflict(results[1 - winner])
        state = self.records.state(account)
        self.assertEqual(state["name"], names[winner], "Losing writer's metadata must roll back")
        self.assertIs(state["extra"][ENABLED], True)
        self.assertFalse(state["schedulable"])
        for key in (DISABLED_AT, PROBE_AT, FAILURE, EXPECTED):
            self.assertNotIn(key, state["extra"])

    def test_replica_admin_only_one_condition_can_commit(self):
        self.concurrent_enables(vendor=False)

    def test_replica_vendor_only_one_condition_can_commit(self):
        self.concurrent_enables(vendor=True)

    def test_replica_reads_follow_committed_enable_and_explicit_stop(self):
        account, stamp = self.records.create_account()
        first = self.assert_success(self.call(account, action="read"))
        self.assertEqual(first["excel_bps_403_recovery"]["disabled_at"], stamp)
        self.assert_success(self.replica_call(account, {"extra": {ENABLED: True, EXPECTED: stamp}}))
        current = self.assert_success(self.call(account, action="read"))
        self.assertIs(current["extra"][ENABLED], True)
        self.assert_success(self.call(account, {"extra": {ENABLED: False}}))
        peer = self.assert_success(self.replica_call(account, action="read"))
        self.assertIs(peer["extra"][ENABLED], False)
        self.assertNotIn(DISABLED_AT, peer["extra"])


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixtures", type=Path, required=True)
    parser.add_argument("--environment", type=Path, required=True)
    parser.add_argument("--expected-image-id", required=True)
    parser.add_argument("--expected-source-sha256", required=True)
    parser.add_argument("--replica-environment", type=Path)
    parser.add_argument("--report", type=Path, required=True)
    args = parser.parse_args(argv)
    records, api, replica_api = None, None, None
    report = {"started_at": datetime.now(timezone.utc).isoformat(),
              "image_id": args.expected_image_id, "source_sha256": args.expected_source_sha256,
              "scope": "Real local Docker HTTP control/PG transactions; system shutdown is seeded, "
                       "not a real upstream rejection or two-gateway recovery proof.",
              "real_upstream_requests": 0}
    started = time.monotonic()
    exit_code = 2
    try:
        fixture = common.read_private_fixture(args.fixtures)
        environment = common.read_private_fixture(args.environment)
        base = common.validate_fixture(fixture, environment, args.expected_image_id,
                                       args.expected_source_sha256)
        def verify_running():
            container = json.loads(subprocess.check_output(
                ["docker", "inspect", environment["api_container_id"]], text=True, timeout=15,
            ))[0]
            common.verify_container(container, environment, base, args.expected_image_id)
        verify_running()
        records = OwnedPostgres(fixture)
        api = BPSControlAPI(base, records)
        BPSControlAcceptance.api, BPSControlAcceptance.records = api, records
        BPSControlAcceptance.fixture = fixture
        suite = unittest.defaultTestLoader.loadTestsFromTestCase(BPSControlAcceptance)
        if args.replica_environment:
            replica = common.read_private_fixture(args.replica_environment)
            replica_fixture = dict(fixture, project=replica["project"],
                                   api_base_url="http://127.0.0.1:" + str(replica["port"]))
            replica_base = common.validate_fixture(
                replica_fixture, replica, args.expected_image_id, args.expected_source_sha256,
            )
            if replica["api_container_id"] == environment["api_container_id"] or replica_base == base:
                raise common.PreconditionError("Replica must be a distinct gateway process and port")
            def verify_replica():
                container = json.loads(subprocess.check_output(
                    ["docker", "inspect", replica["api_container_id"]], text=True, timeout=15,
                ))[0]
                common.verify_container(container, replica, replica_base, args.expected_image_id)
            verify_replica()
            replica_api = BPSControlAPI(replica_base, records)
            BPSReplicaAcceptance.replica = replica_api
            for name in sorted(BPSReplicaAcceptance.__dict__):
                if name.startswith("test_replica_"):
                    suite.addTest(BPSReplicaAcceptance(name))
            report["distinct_gateway_containers"] = [environment["api_container_id"],
                                                     replica["api_container_id"]]
        result = unittest.TextTestRunner(verbosity=2).run(suite)
        report.update(tests_run=result.testsRun, failures=len(result.failures),
                      errors=len(result.errors), skipped=len(result.skipped),
                      failed_tests=[test.id() for test, _ in result.failures + result.errors])
        verify_running()
        if args.replica_environment:
            verify_replica()
        exit_code = 0 if result.wasSuccessful() and result.testsRun and not result.skipped else 1
    except (common.PreconditionError, OSError, ValueError, KeyError, subprocess.SubprocessError) as exc:
        report["precondition_error"] = (str(exc) if isinstance(exc, common.PreconditionError)
                                        else type(exc).__name__)
        print("BLOCKED: " + report["precondition_error"], file=sys.stderr)
    finally:
        if records is not None:
            report["owned_marker"] = records.marker
            report["account_ids"] = records.accounts
            try:
                report["cleanup"] = records.cleanup()
            except (common.PreconditionError, subprocess.SubprocessError):
                report["cleanup_error"] = "Owned cleanup incomplete; preserve exact IDs for recovery"
                exit_code = 1
        if api is not None:
            report["http_observations"] = api.observations
        if replica_api is not None:
            report["replica_http_observations"] = replica_api.observations
        report.update(exit=exit_code, seconds=round(time.monotonic() - started, 3))
        args.report.parent.mkdir(parents=True, exist_ok=True)
        args.report.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n")
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
