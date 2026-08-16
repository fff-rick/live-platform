#!/usr/bin/env python3
import csv
import json
import re
import sys
from pathlib import Path

root = Path(sys.argv[1] if len(sys.argv) > 1 else 'reports/m7/gift-1000-wallet-capacity')
status_re = re.compile(r'^(\S+)\s+([0-9]+)$')
metric_re = re.compile(r'^live_db_pool_(\w+)(?:\{[^}]*\})?\s+([0-9.eE+-]+)$')

def lock_values(path: Path):
    values = {}
    try:
        for line in path.read_text().splitlines():
            m = status_re.match(line.strip())
            if m:
                values[m.group(1)] = int(m.group(2))
    except FileNotFoundError:
        pass
    return values

def pool_metrics(path: Path):
    values = {}
    try:
        for line in path.read_text().splitlines():
            m = metric_re.match(line)
            if m:
                values[m.group(1)] = float(m.group(2))
    except FileNotFoundError:
        pass
    return values

rows = []
for case in sorted(root.glob('wallets-*-pool-*-rate-*'), key=lambda p: int(p.name.rsplit('-', 1)[-1])):
    try:
        report = json.loads((case / 'http.json').read_text())
    except Exception:
        continue
    before = lock_values(case / 'mysql-lock-before.txt')
    after = lock_values(case / 'mysql-lock-after.txt')
    metrics = pool_metrics(case / 'api-metrics.txt')
    peak_in_use = 0.0
    try:
        with (case / 'db-pool-samples.csv').open() as f:
            for row in csv.DictReader(f):
                peak_in_use = max(peak_in_use, float(row.get('in_use') or 0))
    except FileNotFoundError:
        pass
    lock_waits = after.get('Innodb_row_lock_waits', 0) - before.get('Innodb_row_lock_waits', 0)
    lock_ms = after.get('Innodb_row_lock_time', 0) - before.get('Innodb_row_lock_time', 0)
    rows.append({
        'case': case.name,
        'target': report.get('target_rate_per_sec', 0),
        'actual': report.get('achieved_rate_per_sec', 0),
        'p50': report.get('p50', ''),
        'p95': report.get('p95', ''),
        'p99': report.get('p99', ''),
        'failed': report.get('failed', 0),
        'peak': peak_in_use,
        'waits': metrics.get('wait_total', 0),
        'wait_sec': metrics.get('wait_duration_seconds_total', 0),
        'lock_waits': lock_waits,
        'lock_ms': lock_ms,
    })

print('# M7 1000-wallet capacity result')
print()
print('| Target TPS | Actual TPS | P50 | P95 | P99 | Failed | Peak InUse | DB WaitCount | DB Wait sec | Row-lock waits Δ | Row-lock time Δ ms |')
print('|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|')
for r in rows:
    print(f"| {r['target']:.0f} | {r['actual']:.1f} | {r['p50']} | {r['p95']} | {r['p99']} | {r['failed']} | {r['peak']:.0f} | {r['waits']:.0f} | {r['wait_sec']:.3f} | {r['lock_waits']} | {r['lock_ms']} |")
print()
print('Interpretation gate: compare these fixed-pool=40 results with the prior 100-wallet pool=40 cases. If wallet cardinality sharply reduces row-lock waits and raises achieved TPS, the earlier ~500 TPS plateau was workload-hotspot driven rather than an absolute platform ceiling. If throughput remains near the old plateau with low row-lock pressure, profile transaction round trips, commit/storage and MySQL capacity instead.')
