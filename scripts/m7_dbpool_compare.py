#!/usr/bin/env python3
import csv, json, re, sys
from pathlib import Path

root = Path(sys.argv[1] if len(sys.argv) > 1 else 'reports/m7/gift-dbpool-ab')
rows=[]
metric_re=re.compile(r'^live_db_pool_(\w+)(?:\{[^}]*\})?\s+([0-9.eE+-]+)$')
for case in sorted(root.glob('pool-*-rate-*')):
    try:
        report=json.loads((case/'http.json').read_text())
    except Exception:
        continue
    metrics={}
    try:
        for line in (case/'api-metrics.txt').read_text().splitlines():
            m=metric_re.match(line)
            if m: metrics[m.group(1)] = float(m.group(2))
    except FileNotFoundError:
        pass
    peak_in_use=0.0
    try:
        with (case/'db-pool-samples.csv').open() as f:
            for row in csv.DictReader(f):
                peak_in_use=max(peak_in_use,float(row.get('in_use') or 0))
    except FileNotFoundError:
        pass
    rows.append((case.name, report.get('achieved_rate_per_sec',0), report.get('p50',''), report.get('p95',''), report.get('p99',''), peak_in_use, metrics.get('wait_total',0), metrics.get('wait_duration_seconds_total',0), metrics.get('max_open_connections',0)))
print('| Case | Actual TPS | P50 | P95 | P99 | Peak InUse | DB WaitCount | DB Wait sec | MaxOpen |')
print('|---|---:|---:|---:|---:|---:|---:|---:|---:|')
for r in rows:
    print(f'| {r[0]} | {r[1]:.1f} | {r[2]} | {r[3]} | {r[4]} | {r[5]:.0f} | {r[6]:.0f} | {r[7]:.3f} | {r[8]:.0f} |')
