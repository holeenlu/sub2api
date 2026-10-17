import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("cache_audit", Path(__file__).parents[1] / "analyze_gateway_cache.py")
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class CacheAuditTests(unittest.TestCase):
    def event(self, kind, **fields):
        return {"msg": "gateway."+kind, "ts": 100, "correlation_id": "same-session", "client_class": "other", "ingress_protocol": "anthropic", **fields}

    def test_changing_hash_cannot_hide_switches(self):
        events = [self.event("cache_route", ts=100+i, routing_fingerprint="H"+str(i), account_id=i) for i in range(3)]
        row = module.analyze(events)["cohorts"][0]
        self.assertEqual(row["selection_switch_rate"], 1)
        self.assertEqual(row["routing_change_rate"], 1)

    def test_unknown_sessions_and_time_are_not_reconstructed(self):
        events = [self.event("cache_route", correlation_id="", account_id=1), self.event("cache_route", ts=None, account_id=2)]
        row = module.analyze(events)["cohorts"][0]
        self.assertIsNone(row["selection_switch_rate"])
        self.assertEqual(row["uncorrelated_records"], 1)
        self.assertEqual(row["untimed_records"], 1)

    def test_usage_is_token_weighted_deduplicated_and_excludes_discounts(self):
        first = self.event("cache_usage", account_id=1, api_key_id=1, upstream_request_id="r1", has_observed_tokens=True, upstream_input_uncached=10, upstream_cache_read=90, upstream_cache_write=0, billed_cache_read=100)
        second = self.event("cache_usage", ts=101, account_id=2, api_key_id=1, upstream_request_id="r2", has_observed_tokens=True, upstream_input_uncached=0, upstream_cache_read=0, upstream_cache_write=900)
        row = module.analyze([first, first, second])["cohorts"][0]
        self.assertEqual(row["cache_hit_rate"], .09)
        self.assertEqual(row["after_switch_cache_hit_rate"], 0)
        self.assertEqual(row["usage_records"], 2)

    def test_window_boundary_is_not_a_switch(self):
        events = [self.event("cache_route", account_id=1), self.event("cache_route", account_id=2, ts=4000)]
        self.assertIsNone(module.analyze(events)["cohorts"][0]["selection_switch_rate"])


if __name__ == "__main__":
    unittest.main()
