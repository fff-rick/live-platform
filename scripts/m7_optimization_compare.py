#!/usr/bin/env python3
import json
import pathlib
import sys


def load(path):
    with open(path, encoding="utf-8") as f:
        return json.load(f)


def f(v):
    return f"{v:.2f}" if isinstance(v, float) else str(v)


def main():
    root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else "reports/m7/hotroom-adaptive-ab")
    rows = []
    for name in ("baseline", "adaptive"):
        ws = load(root / f"{name}-ws.json")
        http = load(root / f"{name}-http.json")
        clients = max(1, int(ws.get("clients", 1)))
        incoming = float(http.get("achieved_rate_per_sec", 0) or 0)
        fanout = float(ws.get("fanout_delivery_actual_per_sec", 0) or 0)
        broadcast_rate = fanout / clients
        ratio = broadcast_rate / incoming if incoming else 0
        rows.append([name, incoming, broadcast_rate, fanout, ratio, ws.get("latency_p95", ""), ws.get("latency_p99", ""), ws.get("reconnect_events", 0), ws.get("client_errors", 0)])
    print("| mode | incoming msg/s | broadcast msg/s | fan-out/s | effective sample | P95 | P99 | reconnects | client errors |")
    print("|---|---:|---:|---:|---:|---:|---:|---:|---:|")
    for r in rows:
        print("| " + " | ".join(f(x) for x in r) + " |")


if __name__ == "__main__":
    main()
