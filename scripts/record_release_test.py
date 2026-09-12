import tempfile
import unittest
from pathlib import Path

from scripts.record_release import append_event, parse_timestamp


class RecordReleaseTest(unittest.TestCase):
    def test_append_is_jsonl_and_release_id_is_idempotent(self) -> None:
        event = {
            "type": "deployment",
            "occurred_at": "2026-09-12T10:00:00Z",
            "target": "live-api",
            "version": "v2",
            "revision": "abc123",
            "release_id": "ci-42",
        }
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "releases.jsonl"
            self.assertTrue(append_event(output, event))
            self.assertFalse(append_event(output, event))
            self.assertEqual(output.read_text(encoding="utf-8").count("\n"), 1)

    def test_timestamp_is_normalized_and_requires_timezone(self) -> None:
        self.assertEqual(parse_timestamp("2026-09-12T18:00:00+08:00"), "2026-09-12T10:00:00Z")
        with self.assertRaises(Exception):
            parse_timestamp("2026-09-12T18:00:00")


if __name__ == "__main__":
    unittest.main()
