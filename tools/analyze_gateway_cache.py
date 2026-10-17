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



def latency_distribution(values):
    values = sorted(v for v in values if isinstance(v, (int, float)) and not isinstance(v, bool) and math.isfinite(v) and v >= 0)
    def quantile(p):
        if not values:
            return None
        point = (len(values)-1)*p
        lo, hi = math.floor(point), math.ceil(point)
        return values[lo] + (values[hi]-values[lo])*(point-lo)
    return {"n": len(values), "p50": quantile(.5), "p95": quantile(.95)}


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
        key = (*request, record.get("attempt", 1))
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
        cohort = tuple(record.get(k) or "unknown" for k in ("client_class", "client_transport", "transport", "authorization_mode", "model", "reasoning_effort", "requested_service_tier", "actual_service_tier", "request_shape"))
        group = grouped.setdefault(cohort, {"counts": collections.Counter(), "outcomes": collections.Counter(), "incremental_reasons": collections.Counter(), "anchor_reasons": collections.Counter(), "transport_reasons": collections.Counter(), "distributions": collections.defaultdict(list)})
        counts, dist = group["counts"], group["distributions"]
        counts["attempts"] += 1
        group["outcomes"][record.get("outcome", "unknown")] += 1
        markers = record.get("stages_ms") or {}
        for name in ("connection_ready", "wrote_request", "first_event", "first_semantic", "first_content", "first_text", "first_text_flush", "terminal"):
            if name in markers:
                dist[name+"_attempt_ms"].append(markers[name])
        if "first_text" in markers and "wrote_request" in markers:
            dist["write_to_first_text_ms"].append(markers["first_text"]-markers["wrote_request"])
        dist["attempt_duration_ms"].append(record.get("elapsed_ms"))
        detail = stages[key]
        conn = detail.get("got_conn", {})
        if "reused" in conn:
            counts["connection_observed"] += 1
            counts["connection_reused"] += conn["reused"] is True
        for name in ("conn_pick_ms", "queue_wait_ms"):
            if name in conn:
                dist[name].append(conn[name])
        reason = detail.get("transport_decision", {}).get("reason", "unknown")
        group["transport_reasons"][reason] += 1
        if record.get("body_wire_bytes_known") and isinstance(record.get("body_wire_bytes"), (int, float)) and record["body_wire_bytes"] >= 0:
            counts["wire_body_measured_attempts"] += 1
            counts["wire_body_bytes"] += record["body_wire_bytes"]
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
        entries.sort(key=lambda row: row[0][2])
        _, final = entries[-1]
        client_transports[final.get("client_transport") or "unknown"] += 1
        final_transports[final.get("transport") or "unknown"] += 1
        cohort = tuple(final.get(k) or "unknown" for k in ("client_class", "client_transport", "transport", "authorization_mode", "model", "reasoning_effort", "requested_service_tier", "actual_service_tier", "request_shape"))
        group = grouped[cohort]
        group["counts"]["client_requests"] += 1
        group["counts"]["retried_client_requests"] += len(entries) > 1
        group["counts"]["mixed_transport_requests"] += len({r.get("transport") for _, r in entries}) > 1
        summary = requests.get(request)
        if summary:
            group["counts"]["complete_request_timeline"] += 1
            group["distributions"]["request_duration_ms"].append(summary.get("request_duration_ms"))
            for stage, value in (summary.get("stages_ms") or {}).items():
                group["distributions"][stage+"_request_ms"].append(value)
    out = []
    for cohort, group in sorted(grouped.items()):
        counts = group.pop("counts")
        row = dict(zip(("client_class", "client_transport", "transport", "authorization_mode", "model", "reasoning_effort", "requested_service_tier", "actual_service_tier", "request_shape"), cohort))
        row.update(counts)
        row.update({k: dict(v) for k, v in group.items() if k != "distributions"})
        row["timings_ms"] = {k: latency_distribution(v) for k, v in sorted(group["distributions"].items())}
        row["incremental_assessable_rate"] = counts["incremental_assessable"]/counts["incremental_candidates"] if counts["incremental_candidates"] else None
        row["incremental_conditional_match_rate"] = counts["incremental_matches"]/counts["incremental_assessable"] if counts["incremental_assessable"] else None
        row["actual_incremental_bytes_saved"] = 0
        out.append(row)
    return {"timing_version": 2, "cohorts": out, "client_requests": len(by_request), "client_transports": dict(client_transports), "final_upstream_transports": dict(final_transports), "uncorrelated_records": uncorrelated, "duplicate_attempt_summaries": duplicates, "session_transport_share": None, "note": "Unpaired observations; no speedup claim. Session identity is not inferred. Text timing excludes reasoning/tool-only output; missing timings are unknown. Wire bytes measure HTTP body only; WS compressed frame bytes remain unknown. Incremental bytes are uncompressed candidates only; actual incremental savings are zero."}

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("paths", nargs="*", help="JSONL files; omit to read stdin")
    parser.add_argument("--window-seconds", type=int, default=3600)
    parser.add_argument("--latency", action="store_true", help="Summarize timing v2 and passive incremental diagnostics")
    args = parser.parse_args()
    if args.window_seconds <= 0:
        parser.error("--window-seconds must be positive")
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
    result = analyze_latency(records) if args.latency else analyze(records, args.window_seconds)
    result["invalid_lines"] = invalid
    json.dump(result, sys.stdout, ensure_ascii=False, indent=2)
    print()


if __name__ == "__main__":
    main()
