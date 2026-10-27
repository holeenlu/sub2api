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
        result=module.analyze_latency([self.attempt(request="native-request",authorization_mode="native"),self.attempt(request="bps-request",authorization_mode="bps")])
        self.assertEqual(len(result["cohorts"]),2)
        self.assertEqual({row["authorization_mode"] for row in result["cohorts"]},{"native","bps"})

    def test_measured_payload_reduction_is_signed_and_deduplicated(self):
        measured=self.attempt(request="measured-request",incremental_payload={"full_json_bytes":17037,"sent_json_bytes":245})
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


    def test_stage_differences_are_per_attempt_and_reuse_is_observed_only(self):
        base=self.attempt(stages_ms={"connection_ready":10,"wrote_request":30,"first_text":70,"first_text_flush":75})
        records=[base, self.attempt(request="bad", stages_ms={"wrote_request":50,"first_text":40}),
                 self.attempt(request="missing"), self.attempt(request="invalid-flag")]
        for request, reused in (("r1",True),("bad",False),("invalid-flag","true")):
            records.append(self.attempt(request=request, stage="got_conn", reused=reused, conn_pick_ms=4, queue_wait_ms=2, conn_age_ms=200, conn_idle_ms=100))
        records.extend([self.attempt(stage="request_encoding",encoding_ms=3), self.attempt(stage="prewarm",duration_ms=8)])
        row=module.analyze_latency(records)["cohorts"][0]
        for name, value in (("connection_to_write_ms",20),("write_to_first_text_ms",40),("first_text_to_flush_ms",5),("encoding_ms",3),("prewarm_ms",8)):
            self.assertEqual(row["timings_ms"][name], {"n":1,"p50":value,"p95":value})
        self.assertEqual(row["invalid_stage_intervals"],1)
        self.assertEqual(row["timings_ms"]["conn_age_ms"]["p50"],200)
        self.assertEqual(row["timings_ms"]["conn_idle_ms"]["p50"],100)
        self.assertEqual(row["connection_observed"],2)
        self.assertEqual(row["connection_reuse_rate"],.5)
        self.assertIsNone(module.analyze_latency([self.attempt()])["cohorts"][0]["connection_reuse_rate"])


class PairedLatencyAuditTests(unittest.TestCase):
    def receipt(self, arm, pair="p1", **fields):
        controls={k:"fixed" for k in module.PAIR_CONTROLS}
        controls.update(concurrency=1, connection_state="warm", cache_state="warm", measurement_source="local_fixture")
        return {"paired_latency_version":1,"experiment_id":"e1","pair_id":pair,"run_id":pair+arm,
                "arm":arm,"controls":controls,"outcome":"completed",
                "metrics":{"first_text_ms":100 if arm=="base" else 50,"completion_ms":1100,
                           "first_to_last_text_ms":1000,"visible_text_tokens":100},**fields}

    def analyze(self, rows):
        return module.analyze_paired_latency(rows,"base","candidate")

    def test_pairs_keep_signed_deltas_and_failures(self):
        rows=[self.receipt(a) for a in ("base","candidate")]
        rows += [self.receipt("base","p2"),self.receipt("candidate","p2",outcome="timeout",metrics={})]
        result=self.analyze(rows)
        row=result["cohorts"][0]
        self.assertEqual(row["pairs"],2)
        self.assertEqual(row["both_completed"],1)
        self.assertEqual(row["candidate_outcomes"],{"completed":1,"timeout":1})
        self.assertEqual(row["metrics"]["first_text_ms"]["delta"],{"n":1,"p50":-50,"p95":-50})
        self.assertEqual(row["metrics"]["visible_text_tps"]["baseline"]["p50"],100)
        self.assertEqual(row["controls"]["measurement_source"],"local_fixture")

    def test_missing_duplicate_and_reused_runs_are_excluded(self):
        cases=[([self.receipt("base")],"missing_or_duplicate_arm",1),
               ([self.receipt("base"),self.receipt("base"),self.receipt("candidate")],"missing_or_duplicate_arm",1),
               ([self.receipt(a,p,run_id=a) for p in ("p1","p2") for a in ("base","candidate")],"reused_run",2)]
        for rows,reason,count in cases:
            with self.subTest(reason=reason):
                result=self.analyze(rows)
                self.assertEqual(result["cohorts"],[])
                self.assertEqual(result["excluded_pairs"],{reason:count})

    def test_every_control_must_match_and_cold_warm_stay_separate(self):
        for key in module.PAIR_CONTROLS:
            with self.subTest(control=key):
                left,right=self.receipt("base"),self.receipt("candidate")
                right["controls"][key] = (2 if key=="concurrency" else "cold" if key in ("connection_state","cache_state") else "client_receipt" if key=="measurement_source" else "different")
                result=self.analyze([left,right])
                self.assertEqual(result["excluded_pairs"],{"control_mismatch":1})
        rows=[self.receipt(a,p) for p in ("p1","p2") for a in ("base","candidate")]
        for row in rows[2:]:
            row["controls"]["connection_state"]="cold"
        self.assertEqual(len(self.analyze(rows)["cohorts"]),2)

    def test_invalid_or_unknown_controls_are_never_inferred(self):
        for value in (None,"unknown"," UNKNOWN ",True):
            row=self.receipt("base")
            row["controls"]["actual_model"]=value
            self.assertEqual(self.analyze([row])["invalid_records"],1)
        for fields in ({"paired_latency_version":True},{"run_id":""},{"outcome":"partial"},{"metrics":None}):
            self.assertEqual(self.analyze([self.receipt("base",**fields)])["invalid_records"],1)
        with self.assertRaises(ValueError):
            module.analyze_paired_latency([],"same","same")

    def test_invalid_metric_pairs_remain_missing_without_zero_imputation(self):
        for override in ({"visible_text_tokens":True},{"visible_text_tokens":0},{"first_to_last_text_ms":0},
                         {"first_to_last_text_ms":2000},{"visible_text_tokens":None}):
            left,right=self.receipt("base"),self.receipt("candidate")
            right["metrics"].update(override)
            row=self.analyze([left,right])["cohorts"][0]
            self.assertNotIn("visible_text_tps",row["metrics"])
            self.assertEqual(row["missing_or_invalid_metrics"]["visible_text_tps"],1)
            self.assertEqual(row["metrics"]["first_text_ms"]["delta"]["n"],1)
        for override in ({"first_text_ms":float("nan")},{"completion_ms":25},{"first_text_ms":True}):
            left,right=self.receipt("base"),self.receipt("candidate")
            right["metrics"].update(override)
            row=self.analyze([left,right])["cohorts"][0]
            self.assertEqual(row["metrics"],{})
            self.assertEqual(row["both_completed"],1)


if __name__ == "__main__":
    unittest.main()
