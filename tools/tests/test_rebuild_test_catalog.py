"""Keep the versioned acceptance catalog connected to real source and contracts."""

import ast
import json
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[2]
DOCS = ROOT / "docs/architecture-rebuild"


class RebuildTestCatalogTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.catalog = json.loads((DOCS / "test-cases.json").read_text())
        cls.cases = cls.catalog["cases"]

    def test_cases_have_unique_ids_and_actionable_unexecuted_definitions(self):
        ids = [case["id"] for case in self.cases]
        self.assertTrue(ids)
        self.assertEqual(len(ids), len(set(ids)))
        for case in self.cases:
            with self.subTest(case=case["id"]):
                self.assertRegex(case["id"], r"^TC-[A-Z]+-\d{2}$")
                self.assertEqual(case["initial_status"], "not_run")
                self.assertIn(case["priority"], {"P0", "P1", "P2"})
                self.assertIn(case["evidence_level"],
                              {"U", "S", "R", "M", "C", "P", "O", "local_docker", "static_review"})
                for key in ("preconditions", "steps", "expected", "cleanup", "required_evidence"):
                    self.assertTrue(all(isinstance(x, str) and x.strip() for x in case[key]))
                    self.assertGreaterEqual(len(case[key]), 1 if key == "preconditions" else 2)

    def test_acceptance_and_work_package_references_exist(self):
        valid_at = set(re.findall(r"AT-\d{2}", (DOCS / "VALIDATION.md").read_text()))
        work = json.loads((DOCS / "work-items.json").read_text())
        valid_wp = {task["id"] for task in work["tasks"]}
        for case in self.cases:
            with self.subTest(case=case["id"]):
                self.assertTrue(case["acceptance_ids"])
                self.assertTrue(set(case["acceptance_ids"]) <= valid_at)
                self.assertTrue(case["work_packages"])
                self.assertTrue(set(case["work_packages"]) <= valid_wp)

    def test_existing_test_anchors_are_real_and_within_repository(self):
        for case in self.cases:
            with self.subTest(case=case["id"]):
                for path in case["existing_test_files"]:
                    target = (ROOT / path).resolve()
                    self.assertTrue(target.is_relative_to(ROOT))
                    self.assertTrue(target.is_file(), path)
                    self.assertTrue(path.endswith(("_test.go", ".py", ".test.mjs")))
                if case["automation"] == "existing_partial":
                    self.assertTrue(case["existing_test_files"])

    def test_real_upstream_cases_require_separate_authorization(self):
        for case in self.cases:
            if case["evidence_level"] in {"P", "O"}:
                with self.subTest(case=case["id"]):
                    self.assertEqual(case.get("authorization"), "separate_required")

    def test_ready_api_entries_reference_real_unittest_methods(self):
        path = ROOT / "tools/tests/rebuild_api_acceptance.py"
        module = ast.parse(path.read_text())
        owner = next(node for node in module.body if isinstance(node, ast.ClassDef)
                     and node.name == "RebuildAPIAcceptance")
        methods = {owner.name + "." + node.name for node in owner.body
                   if isinstance(node, ast.FunctionDef) and node.name.startswith("test_")}
        entries = {case["automation_entry"] for case in self.cases
                   if case["automation"] == "ready_not_run"}
        self.assertEqual(entries, methods)


if __name__ == "__main__":
    unittest.main()
