#!/usr/bin/env python3
"""Validate the stage-1 Worker deployment split in the inactive K8s base."""
from __future__ import annotations

import pathlib
import sys

import yaml


def fail(message: str) -> None:
    raise SystemExit(f"Phase 1 Kubernetes validation failed: {message}")


root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
base = root / "deploy" / "k8s" / "base"
workloads_path = base / "workloads.yaml"
services_path = base / "services.yaml"

if not workloads_path.is_file() or not services_path.is_file():
    fail("missing Kubernetes base manifests")

workloads = list(yaml.safe_load_all(workloads_path.read_text()))
deployments = {
    item.get("metadata", {}).get("name"): item
    for item in workloads
    if item and item.get("kind") == "Deployment"
}

if "live-worker" in deployments:
    fail("legacy live-worker Deployment must not coexist with split role deployments")

expected = {
    "live-worker-stats": ("stats", False),
    "live-worker-like-snapshot": ("like-snapshot", True),
    "live-worker-outbox": ("outbox", True),
    "live-worker-gift-delivery": ("gift-consumer", True),
    "live-worker-danmaku-archive": ("danmaku-consumer", True),
}
for name, (role, needs_mysql) in expected.items():
    deployment = deployments.get(name)
    if deployment is None:
        fail(f"missing {name}")
    if deployment.get("spec", {}).get("replicas") != 1:
        fail(f"{name} must start at one replica")
    labels = deployment.get("spec", {}).get("template", {}).get("metadata", {}).get("labels", {})
    if labels.get("app.kubernetes.io/component") != "worker":
        fail(f"{name} must carry the worker metrics selector label")
    containers = deployment.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
    if len(containers) != 1:
        fail(f"{name} must contain exactly one Worker container")
    env = {entry.get("name"): entry.get("value") for entry in containers[0].get("env", [])}
    if env.get("WORKER_ROLES") != role:
        fail(f"{name} must run only WORKER_ROLES={role}")
    if needs_mysql and (env.get("MYSQL_MAX_OPEN_CONNS"), env.get("MYSQL_MAX_IDLE_CONNS")) != ("10", "5"):
        fail(f"{name} must use the 10/5 MySQL per-Pod budget")

stats = deployments["live-worker-stats"]
if stats.get("spec", {}).get("strategy", {}).get("type") != "Recreate":
    fail("stats worker must use Recreate until room ownership is distributed")

services = list(yaml.safe_load_all(services_path.read_text()))
metrics_service = next((item for item in services if item and item.get("kind") == "Service" and item.get("metadata", {}).get("name") == "live-worker-metrics"), None)
if metrics_service is None:
    fail("missing live-worker-metrics Service")
if metrics_service.get("spec", {}).get("selector") != {"app.kubernetes.io/component": "worker"}:
    fail("metrics Service selector must cover every split Worker role")

print("Phase 1 Worker split invariants: PASS")
