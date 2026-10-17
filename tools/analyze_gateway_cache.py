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


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("paths", nargs="*", help="JSONL files; omit to read stdin")
    parser.add_argument("--window-seconds", type=int, default=3600)
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
    result = analyze(records, args.window_seconds)
    result["invalid_lines"] = invalid
    json.dump(result, sys.stdout, ensure_ascii=False, indent=2)
    print()


if __name__ == "__main__":
    main()
