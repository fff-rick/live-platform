#!/usr/bin/env python3
import json
import pathlib
import sys


def load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def pick(d, *keys, default=""):
    for key in keys:
        if key in d:
            return d[key]
    return default


def fmt(v):
    if isinstance(v, float):
        return f"{v:.2f}"
    return str(v)


def ws_row(p, d):
    return [
        p.name,
        d.get("scenario", p.stem),
        d.get("clients", ""),
        d.get("rooms", ""),
        pick(d, "connect_rate_target_per_sec", "connect_rate_per_sec"),
        d.get("connect_rate_actual_per_sec", ""),
        pick(d, "publish_rate_target_per_sec", "publish_rate_per_sec"),
        d.get("publish_rate_actual_per_sec", ""),
        d.get("fanout_delivery_actual_per_sec", ""),
        d.get("latency_p95", ""),
        d.get("latency_p99", ""),
        d.get("latency_max", ""),
        d.get("connected_current", ""),
        d.get("reconnect_events", ""),
    ]


def http_row(p, d):
    return [
        p.name,
        d.get("scenario", p.stem),
        d.get("target_rate_per_sec", ""),
        d.get("achieved_rate_per_sec", ""),
        d.get("concurrency", ""),
        d.get("bearer_token_count", ""),
        d.get("sent", ""),
        d.get("failed", ""),
        d.get("p50", ""),
        d.get("p95", ""),
        d.get("p99", ""),
        d.get("max", ""),
    ]


def table(headers, rows):
    print("| " + " | ".join(headers) + " |")
    print("|" + "|".join(["---"] * len(headers)) + "|")
    for row in rows:
        print("| " + " | ".join(fmt(x) for x in row) + " |")


def main():
    if len(sys.argv) < 2:
        raise SystemExit("usage: m7_report_table.py <report.json> [report.json ...]")
    ws, http = [], []
    for arg in sys.argv[1:]:
        p = pathlib.Path(arg)
        d = load(p)
        if "clients" in d:
            ws.append(ws_row(p, d))
        else:
            http.append(http_row(p, d))
    if ws:
        table(
            ["file", "scenario", "clients", "rooms", "conn target/s", "conn actual/s", "pub target/s", "pub actual/s", "fanout/s", "P95", "P99", "max", "connected", "reconnects"],
            ws,
        )
    if ws and http:
        print()
    if http:
        table(
            ["file", "scenario", "target/s", "actual/s", "concurrency", "tokens", "sent", "failed", "P50", "P95", "P99", "max"],
            http,
        )


if __name__ == "__main__":
    main()
