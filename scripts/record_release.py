#!/usr/bin/env python3
"""Append a validated deployment event for AI-SRE release correlation."""

from __future__ import annotations

import argparse
import fcntl
import json
import re
from datetime import UTC, datetime
from pathlib import Path

SERVICE_PATTERN = re.compile(r"^[a-z0-9](?:[-a-z0-9]{0,251}[a-z0-9])?$")
DEFAULT_OUTPUT = Path(__file__).resolve().parents[1] / "reports" / "releases.jsonl"


def parse_timestamp(value: str) -> str:
    parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if parsed.tzinfo is None:
        raise argparse.ArgumentTypeError("timestamp must include a timezone")
    return parsed.astimezone(UTC).isoformat().replace("+00:00", "Z")


def append_event(path: Path, event: dict[str, str]) -> bool:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a+", encoding="utf-8") as stream:
        fcntl.flock(stream, fcntl.LOCK_EX)
        stream.seek(0)
        if event.get("release_id") and any(
            existing.get("release_id") == event["release_id"]
            and existing.get("target") == event["target"]
            for line in stream
            if (existing := _json_object(line)) is not None
        ):
            return False
        stream.seek(0, 2)
        stream.write(json.dumps(event, ensure_ascii=False, separators=(",", ":")) + "\n")
        stream.flush()
    return True


def _json_object(line: str) -> dict[str, object] | None:
    try:
        value = json.loads(line)
    except json.JSONDecodeError:
        return None
    return value if isinstance(value, dict) else None


def main() -> None:
    parser = argparse.ArgumentParser(description="Record one Live Platform deployment")
    parser.add_argument("--service", required=True)
    parser.add_argument("--version", required=True)
    parser.add_argument("--revision", required=True)
    parser.add_argument("--environment", default="local")
    parser.add_argument("--release-id")
    parser.add_argument("--source", default="manual")
    parser.add_argument("--occurred-at", type=parse_timestamp)
    parser.add_argument("--output", type=Path, default=DEFAULT_OUTPUT)
    args = parser.parse_args()
    if not SERVICE_PATTERN.fullmatch(args.service):
        parser.error("--service must be a lowercase DNS-style name")
    event = {
        "type": "deployment",
        "occurred_at": args.occurred_at or datetime.now(UTC).isoformat().replace("+00:00", "Z"),
        "target": args.service,
        "environment": args.environment,
        "version": args.version,
        "revision": args.revision,
        "source": args.source,
    }
    if args.release_id:
        event["release_id"] = args.release_id
    created = append_event(args.output, event)
    print("recorded" if created else "already recorded", json.dumps(event, ensure_ascii=False))


if __name__ == "__main__":
    main()
