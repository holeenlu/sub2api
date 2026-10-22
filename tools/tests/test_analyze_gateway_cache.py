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


class LatencyAuditTests(unittest.TestCase):
    def attempt(self, request="r1", attempt=1, **fields):
        return {"msg":"openai.forward_timing", "timing_version":2, "stage":"attempt_summary", "request_id":request, "turn":1, "attempt":attempt, "client_transport":"http", "transport":"http", "client_class":"codex", "outcome":"completed", "elapsed_ms":100, **fields}

    def test_retries_count_once_and_failures_remain(self):
        first=self.attempt(outcome="error")
        second=self.attempt(attempt=2,transport="websocket",stages_ms={"wrote_request":10,"first_text":50})
        result=module.analyze_latency([first,second,second])
        self.assertEqual(result["client_requests"],1)
        self.assertEqual(result["duplicate_attempt_summaries"],1)
        self.assertEqual(result["final_upstream_transports"],{"websocket":1})
        http=next(c for c in result["cohorts"] if c["transport"]=="http")
        ws=next(c for c in result["cohorts"] if c["transport"]=="websocket")
        self.assertEqual(http["outcomes"],{"error":1})
        self.assertEqual(ws["mixed_transport_requests"],1)
        self.assertEqual(ws["timings_ms"]["write_to_first_text_ms"]["p50"],40)

    def test_missing_values_are_not_zero_and_turns_are_separate(self):
        result=module.analyze_latency([self.attempt(),self.attempt(turn=2),self.attempt(request=""),self.attempt(request="")])
        self.assertEqual(result["client_requests"],4)
        row=result["cohorts"][0]
        self.assertNotIn("first_text_attempt_ms",row["timings_ms"])
        self.assertIsNone(row["incremental_assessable_rate"])
        self.assertIsNone(result["session_transport_share"])

    def test_incremental_coverage_is_not_state_readiness_or_real_savings(self):
        rows=[self.attempt(request="a",incremental_observation={"candidate":True,"assessable":True,"prefix_match":"true","state_ready":"unknown","candidate_semantic_bytes":100}),self.attempt(request="b",incremental_observation={"candidate":True,"assessable":False,"prefix_match":"unknown","reason":"missing_scope"}),self.attempt(request="c",incremental_observation={"candidate":False,"reason":"native_continuation"})]
        row=module.analyze_latency(rows)["cohorts"][0]
        self.assertEqual(row["incremental_assessable_rate"],.5)
        self.assertEqual(row["incremental_conditional_match_rate"],1)
        self.assertEqual(row["incremental_state_ready"],0)
        self.assertIsNone(row["actual_payload_json_bytes_saved"])
        self.assertIsNone(row["actual_wire_bytes_saved"])
        self.assertEqual(row["actual_payload_measured_attempts"],0)
        self.assertEqual(row["candidate_semantic_bytes"],100)

    def test_request_time_includes_wait_and_legacy_logs_are_ignored(self):
        request={"msg":"openai.request_timing","timing_version":2,"request_id":"r1","turn":1,"request_duration_ms":1000,"stages_ms":{"first_text":900,"first_text_flush":901}}
        result=module.analyze_latency([self.attempt(stages_ms={"first_text":80}),request,{"msg":"openai.forward_timing","request_id":"legacy","stage":"attempt_summary"}])
        self.assertEqual(result["client_requests"],1)
        timings=result["cohorts"][0]["timings_ms"]
        self.assertEqual(timings["first_text_request_ms"]["p50"],900)
        self.assertEqual(timings["first_text_attempt_ms"]["p50"],80)
        self.assertEqual(module.latency_distribution([None,-1,float("nan")]),{"n":0,"p50":None,"p95":None})

    def test_authorization_modes_do_not_share_latency_cohorts(self):
        result=module.analyze_latency([self.attempt(request="native",authorization_mode="native"),self.attempt(request="siwc",authorization_mode="siwc")])
        self.assertEqual(len(result["cohorts"]),2)
        self.assertEqual({row["authorization_mode"] for row in result["cohorts"]},{"native","siwc"})

    def test_measured_payload_reduction_is_signed_and_deduplicated(self):
        measured=self.attempt(request="siwc",incremental_payload={"full_json_bytes":17037,"sent_json_bytes":245})
        rows=[measured,measured,
              self.attempt(request="overhead",incremental_payload={"full_json_bytes":10,"sent_json_bytes":20}),
              self.attempt(request="unknown"),
              self.attempt(request="invalid",incremental_payload={"full_json_bytes":True,"sent_json_bytes":1}),
              self.attempt(request="missing",incremental_payload={"full_json_bytes":100})]
        row=module.analyze_latency(rows)["cohorts"][0]
        self.assertEqual(row["actual_payload_measured_attempts"],2)
        self.assertEqual(row["actual_payload_json_bytes_saved"],16782)
        self.assertEqual(row["full_payload_json_bytes"],17047)
        self.assertEqual(row["sent_payload_json_bytes"],265)
        self.assertIsNone(row["actual_wire_bytes_saved"])
        self.assertNotIn("actual_incremental_bytes_saved",row)
        zero=module.analyze_latency([self.attempt(incremental_payload={"full_json_bytes":10,"sent_json_bytes":10})])["cohorts"][0]
        self.assertEqual(zero["actual_payload_json_bytes_saved"],0)

    def test_models_protocols_and_timing_scopes_remain_separate(self):
        common={"model":"claude-alias","protocol":"anthropic","timing_scope":"response_stream"}
        rows=[self.attempt(request="opus",actual_model="claude-opus",**common),
              self.attempt(request="sonnet",actual_model="claude-sonnet",**common),
              self.attempt(request="unknown",**common),
              self.attempt(request="responses",model="claude-alias",actual_model="claude-opus",protocol="responses",timing_scope="upstream_attempt"),
              self.attempt(request="other-scope",model="claude-alias",actual_model="claude-opus",protocol="anthropic",timing_scope="upstream_attempt")]
        result=module.analyze_latency(rows)
        self.assertEqual(len(result["cohorts"]),5)
        self.assertEqual({r["actual_model"] for r in result["cohorts"]},{"claude-opus","claude-sonnet","unknown"})

    def test_nested_scope_is_not_a_retry_or_full_request_timeline(self):
        outer=self.attempt(protocol="responses",timing_scope="upstream_attempt",stages_ms={"first_text":100})
        inner=self.attempt(protocol="anthropic",timing_scope="response_stream",stages_ms={"first_text":5})
        summary={"msg":"openai.request_timing","timing_version":2,"request_id":"r1","turn":1,"request_duration_ms":200,"stages_ms":{"first_text":150}}
        result=module.analyze_latency([outer,inner,summary])
        self.assertEqual(result["duplicate_attempt_summaries"],0)
        self.assertEqual(result["client_requests"],1)
        self.assertEqual(len(result["cohorts"]),2)
        stream=next(c for c in result["cohorts"] if c["timing_scope"]=="response_stream")
        upstream=next(c for c in result["cohorts"] if c["timing_scope"]=="upstream_attempt")
        self.assertNotIn("first_text_request_ms",stream["timings_ms"])
        self.assertEqual(upstream["retried_client_requests"],0)
        self.assertEqual(upstream["timings_ms"]["first_text_request_ms"]["p50"],150)
        only_stream=module.analyze_latency([inner,summary])["cohorts"][0]
        self.assertNotIn("first_text_request_ms",only_stream["timings_ms"])


if __name__ == "__main__":
    unittest.main()
