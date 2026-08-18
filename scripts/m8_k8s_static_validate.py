#!/usr/bin/env python3
"""Validate the Kubernetes manifests that the current Argo CD app actually uses."""
from __future__ import annotations

import pathlib
import sys

import yaml


def fail(message: str) -> None:
    raise SystemExit(f"GitOps static validation failed: {message}")


root = pathlib.Path(sys.argv[1] if len(sys.argv) > 1 else ".").resolve()
k8s = root / "deploy" / "k8s"
overlay = k8s / "overlays" / "demo"

for required in (
    k8s / "README.md",
    k8s / "argocd" / "live-platform-app.yaml",
    overlay / "kustomization.yaml",
    overlay / "sealed-runtime.yaml",
):
    if not required.is_file():
        fail(f"missing required GitOps file: {required.relative_to(root)}")

app = yaml.safe_load((k8s / "argocd" / "live-platform-app.yaml").read_text())
if app.get("kind") != "Application" or app.get("metadata", {}).get("name") != "live-platform-demo":
    fail("expected Argo CD Application/live-platform-demo")
source = app.get("spec", {}).get("source", {})
destination = app.get("spec", {}).get("destination", {})
if source.get("path") != "deploy/k8s/overlays/demo":
    fail("Argo CD source path must be deploy/k8s/overlays/demo")
if source.get("targetRevision") != "main" or destination.get("namespace") != "live-platform":
    fail("Argo CD app must target main in namespace live-platform")

kustomization = yaml.safe_load((overlay / "kustomization.yaml").read_text())
resources = kustomization.get("resources", [])
if kustomization.get("namespace") != "live-platform":
    fail("demo overlay namespace must be live-platform")
if resources != ["sealed-runtime.yaml"]:
    fail("current demo overlay must manage only sealed-runtime.yaml; do not claim legacy workloads are deployed")
images = kustomization.get("images", [])
if len(images) != 1 or images[0].get("name") != "live-platform":
    fail("demo overlay must contain exactly one live-platform image declaration")
tag = images[0].get("newTag", "")
if not isinstance(tag, str) or not tag.startswith("v"):
    fail("demo image tag must be an explicit version beginning with v")

sealed = yaml.safe_load((overlay / "sealed-runtime.yaml").read_text())
if sealed.get("kind") != "SealedSecret" or sealed.get("metadata", {}).get("name") != "live-platform-runtime":
    fail("demo runtime material must be SealedSecret/live-platform-runtime")

print("GitOps demo invariants: PASS")
