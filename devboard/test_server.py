"""Tests for the friction-log parser in server.py.

Run from the devboard directory:  python3 -m unittest test_server -v

The Go side (worklog/internal/feedback) owns the FEEDBACK.md format; this
suite pins the reader against it, since the two parsers can drift.
"""

import importlib
import os
import tempfile
import unittest


def load_server(worklog_dir):
    """Import server.py with DEVBOARD_WORKLOG pointed at a fixture dir.

    server.py reads its config into module-level constants at import time,
    so each case re-imports rather than mutating globals.
    """
    os.environ["DEVBOARD_WORKLOG"] = worklog_dir
    os.environ.setdefault("DEVBOARD_DATA", os.path.join(worklog_dir, "_nodata"))
    import server
    return importlib.reload(server)


class FeedbackParserTest(unittest.TestCase):
    def parse(self, content=None):
        with tempfile.TemporaryDirectory() as d:
            if content is not None:
                with open(os.path.join(d, "FEEDBACK.md"), "w", encoding="utf-8") as fh:
                    fh.write(content)
            return load_server(d)._parse_feedback()

    def test_missing_file_is_empty_not_an_error(self):
        self.assertEqual(self.parse(), [])

    def test_header_only_file(self):
        self.assertEqual(self.parse("# Worklog Feedback Log\n\n"), [])

    def test_full_entry(self):
        entries = self.parse(
            "# Worklog Feedback Log\n"
            "\n"
            "## 1788310587 — missing-feature\n"
            "**Trigger**: worklog task refuses every command in a worktree\n"
            "**Excerpt**:\n"
            "> line one\n"
            "> line two\n"
            "**Context**: dispatcher note\n"
            "\n"
        )
        self.assertEqual(len(entries), 1)
        e = entries[0]
        self.assertEqual(e["timestamp"], 1788310587)
        self.assertEqual(e["signal"], "missing-feature")
        self.assertEqual(e["trigger"], "worklog task refuses every command in a worktree")
        self.assertEqual(e["excerpt"], "line one\nline two")
        self.assertEqual(e["context"], "dispatcher note")
        self.assertEqual(e["resolved"], 0)

    def test_resolved_entry(self):
        entries = self.parse(
            "# Worklog Feedback Log\n"
            "\n"
            "## 1000 — tui-error\n"
            "**Trigger**: t\n"
            "**Resolved**: 2000\n"
            "\n"
        )
        self.assertEqual(entries[0]["resolved"], 2000)

    def test_multiple_entries_keep_file_order(self):
        entries = self.parse(
            "# Worklog Feedback Log\n"
            "\n"
            "## 1000 — tui-error\n"
            "**Trigger**: first\n"
            "\n"
            "## 2000 — profanity\n"
            "**Trigger**: second\n"
            "**Resolved**: 3000\n"
            "\n"
        )
        self.assertEqual([e["timestamp"] for e in entries], [1000, 2000])
        self.assertEqual([e["resolved"] for e in entries], [0, 3000])

    def test_unknown_field_is_skipped_and_entry_survives(self):
        entries = self.parse(
            "## 1000 — tui-error\n"
            "**Trigger**: t\n"
            "**SomethingNew**: from a future worklog\n"
            "**Context**: c\n"
        )
        self.assertEqual(len(entries), 1)
        self.assertEqual(entries[0]["trigger"], "t")
        self.assertEqual(entries[0]["context"], "c")

    def test_malformed_heading_is_ignored(self):
        # A plain "## heading" is not an entry; the real entry still parses.
        entries = self.parse(
            "## not an entry\n"
            "**Trigger**: orphaned\n"
            "\n"
            "## 1000 — tui-error\n"
            "**Trigger**: real\n"
        )
        self.assertEqual(len(entries), 1)
        self.assertEqual(entries[0]["trigger"], "real")

    def test_non_numeric_resolved_is_treated_as_unresolved(self):
        entries = self.parse(
            "## 1000 — tui-error\n"
            "**Trigger**: t\n"
            "**Resolved**: yesterday\n"
        )
        self.assertEqual(entries[0]["resolved"], 0)

    def test_garbage_file_yields_no_entries_rather_than_raising(self):
        self.assertEqual(self.parse("\x00 not markdown at all \xff"), [])


class ApiPayloadTest(unittest.TestCase):
    def test_feedback_key_present_when_file_absent(self):
        with tempfile.TemporaryDirectory() as d:
            server = load_server(d)
            payload = server._all_tasks()
        self.assertIn("feedback", payload)
        self.assertEqual(payload["feedback"], [])
        self.assertIn("repos", payload)

    def test_feedback_key_populated(self):
        with tempfile.TemporaryDirectory() as d:
            with open(os.path.join(d, "FEEDBACK.md"), "w", encoding="utf-8") as fh:
                fh.write("## 1000 — tui-error\n**Trigger**: t\n")
            server = load_server(d)
            payload = server._all_tasks()
        self.assertEqual(len(payload["feedback"]), 1)
        self.assertEqual(payload["feedback"][0]["signal"], "tui-error")


if __name__ == "__main__":
    unittest.main()
