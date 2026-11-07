#!/usr/bin/env python3
"""Read gateway JSONL diagnostics without querying or modifying production data.

Correlate by the independent, tenant-scoped correlation_id, never by the routing
fingerprint under investigation. Selection changes are not upstream executions.
Rates are token weighted; a missing conversation or timestamp remains unknown.
"""
import argparse
import collections
import datetime
import json
import math
import sys


def timestamp(record):
    value = record.get("ts", record.get("time", record.get("timestamp")))
    if isinstance(value, (int, float)):
        return float(value)
    if isinstance(value, str):
        try:
            parsed = datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
            if parsed.tzinfo is None:
                return None
            return parsed.timestamp()
        except ValueError:
            pass
    return None


def analyze(records, window_seconds=3600):
    cohorts = collections.defaultdict(lambda: collections.Counter())
    routes, prefixes = collections.defaultdict(list), collections.defaultdict(list)
    usage = {}
    for index, record in enumerate(records):
        event = record.get("msg", record.get("message", ""))
        if event not in ("gateway.cache_route", "gateway.cache_usage", "gateway.cache_prefix_transform"):
            continue
        cohort = (record.get("ingress_protocol", "unknown"), record.get("client_class", "unknown"))
        counts = cohorts[cohort]
        session, ts = record.get("correlation_id"), timestamp(record)
        if not session:
            counts["uncorrelated_records"] += 1
        if ts is None:
            counts["untimed_records"] += 1
        if event == "gateway.cache_route":
            counts["selection_records"] += 1
            if session and ts is not None:
                routes[(cohort, session)].append((ts, index, record))
        elif event == "gateway.cache_prefix_transform":
            counts["prefix_records"] += 1
            counts["transformed_prefixes"] += bool(record.get("prefix_changed"))
            if session and ts is not None:
                prefixes[(cohort, session, record.get("account_id"), record.get("model"))].append((ts, index, record))
        else:
            request = record.get("upstream_request_id")
            if not request:
                counts["usage_without_upstream_id"] += 1
            key = (record.get("api_key_id"), record.get("account_id"), request) if request else ("unidentified", index)
            if key in usage:
                counts["duplicate_usage_records"] += 1
            # Later snapshots replace partial/repeated records, not add to them.
            prior = usage.get(key)
            rank = (ts if ts is not None else float("-inf"), index)
            if prior is None or rank >= prior[0]:
                usage[key] = (rank, cohort, record)

    for (cohort, _), entries in routes.items():
        counts = cohorts[cohort]
        counts["correlated_sessions"] += 1
        for previous, current in zip(sorted(entries), sorted(entries)[1:]):
            if current[0] - previous[0] > window_seconds:
                continue
            counts["selection_transitions"] += 1
            counts["account_switches"] += current[2].get("account_id") != previous[2].get("account_id")
            counts["routing_changes"] += current[2].get("routing_fingerprint") != previous[2].get("routing_fingerprint")
    for (cohort, *_), entries in prefixes.items():
        ordered = sorted(entries)
        for previous, current in zip(ordered, ordered[1:]):
            if current[0] - previous[0] <= window_seconds:
                cohorts[cohort]["outbound_prefix_changes"] += current[2].get("prefix_after") != previous[2].get("prefix_after")

    usage_sessions = collections.defaultdict(list)
    for rank, cohort, record in usage.values():
        counts = cohorts[cohort]
        counts["usage_records"] += 1
        if not record.get("has_observed_tokens"):
            counts["usage_zero_or_missing"] += 1
            continue
        uncached, read, write = (max(0, int(record.get(k, 0))) for k in ("upstream_input_uncached", "upstream_cache_read", "upstream_cache_write"))
        counts["input_tokens"] += uncached + read + write
        counts["cache_read_tokens"] += read
        counts["cache_write_tokens"] += write
        counts["output_tokens"] += max(0, int(record.get("upstream_output", 0)))
        if record.get("correlation_id") and timestamp(record) is not None:
            usage_sessions[(cohort, record["correlation_id"])].append((rank, record, uncached+read+write, read))
    for (cohort, _), entries in usage_sessions.items():
        ordered = sorted(entries, key=lambda row: row[0])
        for previous, current in zip(ordered, ordered[1:]):
            if current[0][0] - previous[0][0] <= window_seconds and current[1].get("account_id") != previous[1].get("account_id"):
                cohorts[cohort]["after_account_switch_input"] += current[2]
                cohorts[cohort]["after_account_switch_cache_read"] += current[3]
    out = []
    for (protocol, client), counts in sorted(cohorts.items()):
        row = {"protocol": protocol, "client_class": client, **counts}
        for name, numerator, denominator in (("cache_hit_rate", "cache_read_tokens", "input_tokens"), ("selection_switch_rate", "account_switches", "selection_transitions"), ("routing_change_rate", "routing_changes", "selection_transitions"), ("after_switch_cache_hit_rate", "after_account_switch_cache_read", "after_account_switch_input")):
            row[name] = counts[numerator] / counts[denominator] if counts[denominator] else None
        out.append(row)
    return {"window_seconds": window_seconds, "cohorts": out}



def latency_distribution(values, signed=False):
    values = sorted(v for v in values if isinstance(v, (int, float)) and not isinstance(v, bool) and math.isfinite(v) and (signed or v >= 0))
    def quantile(p):
        if not values:
            return None
        point = (len(values)-1)*p
        lo, hi = math.floor(point), math.ceil(point)
        return values[lo] + (values[hi]-values[lo])*(point-lo)
    return {"n": len(values), "p50": quantile(.5), "p95": quantile(.95)}


LATENCY_COHORT_FIELDS = ("protocol", "timing_scope", "client_class", "client_transport", "transport",
                         "authorization_mode", "model", "actual_model", "reasoning_effort",
                         "requested_service_tier", "actual_service_tier", "request_shape")


def analyze_latency(records):
    """Timing v2 only. Missing observations stay unknown, failures stay counted.

    These are unpaired diagnostic cohorts, never causal speedup estimates.
    Request text time starts at middleware entry; attempt time starts before
    connection acquisition. Local flush is not client receipt/display time.
    """
    attempts, requests, stages = {}, {}, collections.defaultdict(dict)
    uncorrelated = duplicates = 0
    for index, record in enumerate(records):
        event = record.get("msg", record.get("message"))
        if record.get("timing_version") != 2 or event not in ("openai.forward_timing", "openai.request_timing"):
            continue
        request_id = record.get("request_id")
        # Never combine requests whose identity is absent.
        if not request_id:
            uncorrelated += 1
            request_id = ("unknown", index)
        request = (request_id, record.get("turn", 1))
        if event == "openai.request_timing":
            requests[request] = record
            continue
        key = (*request, record.get("attempt", 1), record.get("protocol") or "unknown", record.get("timing_scope") or "unknown")
        stage = record.get("stage")
        if stage == "attempt_summary":
            duplicates += key in attempts
            attempts[key] = record
        elif stage in ("got_conn", "request_encoding", "transport_decision", "prewarm"):
            stages[key][stage] = record
    grouped = {}
    by_request = collections.defaultdict(list)
    for key, record in attempts.items():
        by_request[key[:2]].append((key, record))
        cohort = tuple(record.get(k) or "unknown" for k in LATENCY_COHORT_FIELDS)
        group = grouped.setdefault(cohort, {"counts": collections.Counter(), "outcomes": collections.Counter(), "incremental_reasons": collections.Counter(), "anchor_reasons": collections.Counter(), "transport_reasons": collections.Counter(), "distributions": collections.defaultdict(list)})
        counts, dist = group["counts"], group["distributions"]
        counts["attempts"] += 1
        group["outcomes"][record.get("outcome", "unknown")] += 1
        markers = record.get("stages_ms") or {}
        for name in ("connection_ready", "wrote_request", "first_event", "first_semantic", "first_content", "first_text", "first_text_flush", "terminal"):
            if name in markers:
                dist[name+"_attempt_ms"].append(markers[name])
        for name, start, end in (("connection_to_write_ms", "connection_ready", "wrote_request"),
                                 ("write_to_first_text_ms", "wrote_request", "first_text"),
                                 ("first_text_to_flush_ms", "first_text", "first_text_flush")):
            left, right = markers.get(start), markers.get(end)
            if all(type(v) in (int, float) and math.isfinite(v) and v >= 0 for v in (left, right)):
                if right >= left:
                    dist[name].append(right-left)
                else:
                    counts["invalid_stage_intervals"] += 1
        dist["attempt_duration_ms"].append(record.get("elapsed_ms"))
        detail = stages[key]
        conn = detail.get("got_conn", {})
        if type(conn.get("reused")) is bool:
            counts["connection_observed"] += 1
            counts["connection_reused"] += conn["reused"] is True
        for name in ("conn_pick_ms", "queue_wait_ms", "conn_age_ms", "conn_idle_ms"):
            if name in conn:
                dist[name].append(conn[name])
        for stage, source, target in (("request_encoding", "encoding_ms", "encoding_ms"),
                                      ("prewarm", "duration_ms", "prewarm_ms")):
            value = detail.get(stage, {}).get(source)
            if value is not None:
                dist[target].append(value)
        reason = detail.get("transport_decision", {}).get("reason", "unknown")
        group["transport_reasons"][reason] += 1
        if record.get("body_wire_bytes_known") and isinstance(record.get("body_wire_bytes"), (int, float)) and record["body_wire_bytes"] >= 0:
            counts["wire_body_measured_attempts"] += 1
            counts["wire_body_bytes"] += record["body_wire_bytes"]
        payload = record.get("incremental_payload")
        if isinstance(payload, dict):
            full, sent = payload.get("full_json_bytes"), payload.get("sent_json_bytes")
            if type(full) is int and type(sent) is int and full >= 0 and sent >= 0:
                counts["actual_payload_measured_attempts"] += 1
                counts["full_payload_json_bytes"] += full
                counts["sent_payload_json_bytes"] += sent
        observation = record.get("incremental_observation")
        if isinstance(observation, dict):
            counts["incremental_observed"] += 1
            group["incremental_reasons"][observation.get("reason", "unknown")] += 1
            group["anchor_reasons"][observation.get("anchor_reason", "unknown")] += 1
            if observation.get("candidate") is True:
                counts["incremental_candidates"] += 1
                counts["incremental_assessable"] += observation.get("assessable") is True
                counts["incremental_matches"] += observation.get("prefix_match") == "true"
                counts["incremental_state_ready"] += observation.get("state_ready") == "true"
                counts["candidate_semantic_bytes"] += max(0, observation.get("candidate_semantic_bytes", 0))
    # Count each client generation once regardless of gateway retry count; assign
    # mixed-transport requests to their last attempt and expose that ambiguity.
    client_transports, final_transports = collections.Counter(), collections.Counter()
    for request, entries in by_request.items():
        # A nested stream observation is not another upstream retry. Prefer
        # the complete upstream attempt when both scopes describe one request.
        entries.sort(key=lambda row: (row[0][2], row[1].get("timing_scope") != "response_stream"))
        _, final = entries[-1]
        client_transports[final.get("client_transport") or "unknown"] += 1
        final_transports[final.get("transport") or "unknown"] += 1
        cohort = tuple(final.get(k) or "unknown" for k in LATENCY_COHORT_FIELDS)
        group = grouped[cohort]
        group["counts"]["client_requests"] += 1
        group["counts"]["retried_client_requests"] += len({key[2] for key, _ in entries}) > 1
        group["counts"]["mixed_transport_requests"] += len({r.get("transport") for _, r in entries}) > 1
        summary = requests.get(request)
        if summary and final.get("timing_scope") != "response_stream":
            group["counts"]["complete_request_timeline"] += 1
            group["distributions"]["request_duration_ms"].append(summary.get("request_duration_ms"))
            for stage, value in (summary.get("stages_ms") or {}).items():
                group["distributions"][stage+"_request_ms"].append(value)
    out = []
    for cohort, group in sorted(grouped.items()):
        counts = group.pop("counts")
        row = dict(zip(LATENCY_COHORT_FIELDS, cohort))
        row.update(counts)
        row.update({k: dict(v) for k, v in group.items() if k != "distributions"})
        row["timings_ms"] = {k: latency_distribution(v) for k, v in sorted(group["distributions"].items())}
        row["incremental_assessable_rate"] = counts["incremental_assessable"]/counts["incremental_candidates"] if counts["incremental_candidates"] else None
        row["incremental_conditional_match_rate"] = counts["incremental_matches"]/counts["incremental_assessable"] if counts["incremental_assessable"] else None
        observed = counts["connection_observed"]
        row["connection_reuse_rate"] = counts["connection_reused"]/observed if observed else None
        measured = counts["actual_payload_measured_attempts"]
        row["actual_payload_measured_attempts"] = measured
        row["actual_payload_json_bytes_saved"] = (counts["full_payload_json_bytes"] - counts["sent_payload_json_bytes"]) if measured else None
        row["actual_wire_bytes_saved"] = None
        out.append(row)
    return {"timing_version": 2, "cohorts": out, "client_requests": len(by_request), "client_transports": dict(client_transports), "final_upstream_transports": dict(final_transports), "uncorrelated_records": uncorrelated, "duplicate_attempt_summaries": duplicates, "session_transport_share": None, "note": "Unpaired observations; no speedup claim. Session identity is not inferred. Text timing excludes reasoning/tool-only output; missing timings are unknown. Wire bytes measure HTTP body only; WS compressed frame bytes remain unknown. P2 candidate bytes are separate from measured JSON payload reduction after successful writes; negative reduction is overhead. Missing measurements and compressed frame/TLS savings are unknown."}

# Client receipts, not server timestamps: each arm owns independent conversation
# state. Pair membership and controls are explicit; never inferred from prompts,
# cache hits, provider response IDs, or proximity in time. No requests are sent.
PAIR_CONTROLS = (
    "task_digest", "tools_digest", "actual_model", "reasoning_effort",
    "actual_service_tier", "authorization_mode", "account_cohort", "egress",
    "concurrency", "connection_state", "cache_state", "compression",
    "client_version", "tokenizer", "measurement_source",
)


def analyze_paired_latency(records, baseline, candidate):
    if not baseline or not candidate or baseline == candidate:
        raise ValueError("Choose two distinct nonempty arms.")
    pairs, invalid = collections.defaultdict(lambda: collections.defaultdict(list)), 0
    for record in records:
        if record.get("arm") not in (baseline, candidate):
            continue
        ids = [record.get(k) for k in ("experiment_id", "pair_id", "run_id")]
        controls, metrics = record.get("controls"), record.get("metrics")
        valid = (type(record.get("paired_latency_version")) is int and record["paired_latency_version"] == 1 and
                 all(isinstance(v, str) and v.strip() for v in ids) and
                 isinstance(controls, dict) and isinstance(metrics, dict) and
                 record.get("outcome") in ("completed", "error", "cancelled", "timeout"))
        if valid:
            valid = all(isinstance(controls.get(k), str) and controls[k].strip() and
                        controls[k].strip().lower() != "unknown" for k in PAIR_CONTROLS if k != "concurrency")
            valid = valid and type(controls.get("concurrency")) is int and controls["concurrency"] > 0
            valid = valid and controls["connection_state"] in ("cold", "warm")
            valid = valid and controls["cache_state"] in ("cold", "warm")
            valid = valid and controls["measurement_source"] in ("client_receipt", "local_fixture")
        if not valid:
            invalid += 1
            continue
        pairs[tuple(ids[:2])][record["arm"]].append(record)
    exclusions, cohorts = collections.Counter(), {}
    run_uses = collections.Counter((r["experiment_id"], r["run_id"])
                                   for arms in pairs.values() for rows in arms.values() for r in rows)
    for (experiment, _), arms in pairs.items():
        if any(len(arms[arm]) != 1 for arm in (baseline, candidate)):
            exclusions["missing_or_duplicate_arm"] += 1
            continue
        left, right = arms[baseline][0], arms[candidate][0]
        if any(run_uses[(experiment, r["run_id"])] != 1 for r in (left, right)):
            exclusions["reused_run"] += 1
            continue
        if any(left["controls"][k] != right["controls"][k] for k in PAIR_CONTROLS):
            exclusions["control_mismatch"] += 1
            continue
        key = (experiment, *(left["controls"][k] for k in PAIR_CONTROLS))
        group = cohorts.setdefault(key, {"pairs": 0, "both_completed": 0,
            "baseline_outcomes": collections.Counter(), "candidate_outcomes": collections.Counter(),
            "metrics": collections.defaultdict(lambda: {"baseline": [], "candidate": [], "delta": []}),
            "missing_or_invalid_metrics": collections.Counter()})
        group["pairs"] += 1
        group["baseline_outcomes"][left["outcome"]] += 1
        group["candidate_outcomes"][right["outcome"]] += 1
        if left["outcome"] != "completed" or right["outcome"] != "completed":
            continue  # Retained in outcomes, never silently counted as fast completions.
        group["both_completed"] += 1
        def measured(row, name):
            m = row["metrics"]
            def number(k):
                v = m.get(k)
                return v if type(v) in (int, float) and math.isfinite(v) and v >= 0 else None
            first, complete = number("first_text_ms"), number("completion_ms")
            if first is None or complete is None or complete < first:
                return None
            if name == "visible_text_tps":
                tokens, window = m.get("visible_text_tokens"), number("first_to_last_text_ms")
                if type(tokens) is not int or tokens <= 0 or window is None or window <= 0 or window > complete-first:
                    return None
                return tokens*1000/window
            return first if name == "first_text_ms" else complete
        for name in ("first_text_ms", "completion_ms", "visible_text_tps"):
            a, b = measured(left, name), measured(right, name)
            if a is None or b is None:
                group["missing_or_invalid_metrics"][name] += 1
                continue
            group["metrics"][name]["baseline"].append(a)
            group["metrics"][name]["candidate"].append(b)
            group["metrics"][name]["delta"].append(b-a)
    rows = []
    for key, group in sorted(cohorts.items()):
        rows.append({"experiment_id": key[0], "controls": dict(zip(PAIR_CONTROLS, key[1:])),
                     **group, "metrics": {name: {label: latency_distribution(values, signed=label == "delta")
                                  for label, values in stats.items()} for name, stats in group["metrics"].items()}})
    return {"paired_latency_version": 1, "baseline_arm": baseline, "candidate_arm": candidate,
            "invalid_records": invalid, "excluded_pairs": dict(exclusions), "cohorts": rows,
            "note": "Deltas are candidate minus baseline for the same explicit pair. Controls are supplied by the experiment owner, not independently verified. Failures stay in denominators. Client first text includes network/flush; visible-text TPS excludes hidden reasoning and is not billing TPS. Local fixtures never establish provider speedup."}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("paths", nargs="*", help="JSONL files; omit to read stdin")
    parser.add_argument("--window-seconds", type=int, default=3600)
    parser.add_argument("--latency", action="store_true", help="Summarize timing v2 and passive incremental diagnostics")
    parser.add_argument("--paired-latency", nargs=2, metavar=("BASELINE", "CANDIDATE"), help="Compare explicit controlled client receipts; sends no requests")
    args = parser.parse_args()
    if args.window_seconds <= 0:
        parser.error("--window-seconds must be positive")
    if args.paired_latency and (args.latency or args.paired_latency[0] == args.paired_latency[1]):
        parser.error("Use --paired-latency with distinct arms, separately from --latency")
    records, invalid = [], 0
    streams = [open(path, encoding="utf-8") for path in args.paths] if args.paths else [sys.stdin]
    try:
        for stream in streams:
            for line in stream:
                try:
                    record = json.loads(line)
                    if not isinstance(record, dict):
                        raise ValueError("not an object")
                    records.append(record)
                except (ValueError, TypeError):
                    invalid += 1
    finally:
        for stream in streams:
            if stream is not sys.stdin:
                stream.close()
    result = (analyze_paired_latency(records, *args.paired_latency) if args.paired_latency else
              analyze_latency(records) if args.latency else analyze(records, args.window_seconds))
    result["invalid_lines"] = invalid
    json.dump(result, sys.stdout, ensure_ascii=False, indent=2)
    print()


if __name__ == "__main__":
    main()
