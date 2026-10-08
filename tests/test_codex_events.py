"""Codex event/driver regressions; use a local CLI fixture, never model inference."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location("codex_events", ROOT / "bin/lib/codex-events.py")
events = importlib.util.module_from_spec(spec)
spec.loader.exec_module(events)


class EventTests(unittest.TestCase):
    def inspect(self, stream, response=""):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder)
            (path / "events").write_text("\n".join(json.dumps(item) if isinstance(item, dict) else item for item in stream))
            (path / "response").write_text(response)
            return events.inspect_events(path / "events", path / "response")

    def test_completion_records_thread_usage_and_commands(self):
        metadata, response = self.inspect([
            {"type": "thread.started", "thread_id": "thread-a"},
            {"type": "item.completed", "item": {"type": "command_execution", "command": "false", "exit_code": 1}},
            {"type": "turn.completed", "usage": {"input_tokens": 12, "cached_input_tokens": 8, "output_tokens": 3}},
        ], "A failing test was found")
        self.assertTrue(metadata["complete"])
        self.assertEqual(metadata["thread_id"], "thread-a")
        self.assertEqual(metadata["commands"][0]["exit_code"], 1)
        self.assertEqual(metadata["usage"][0]["cached_input_tokens"], 8)
        self.assertEqual(response, "A failing test was found")

    def test_event_message_can_supply_missing_review_output(self):
        metadata, response = self.inspect([
            {"type": "item.completed", "item": {"type": "agent_message", "text": "finding"}},
            {"type": "turn.completed"},
        ])
        self.assertTrue(metadata["complete"])
        self.assertEqual(response, "finding")
        self.assertEqual(metadata["response_source"], "agent_message_event")

    def test_incomplete_failure_empty_and_malformed_are_not_success(self):
        for stream, response in [
            ([], "partial reply"),
            ([{"type": "turn.completed"}], ""),
            ([{"type": "turn.failed"}, {"type": "turn.completed"}], "reply"),
            ([{"type": "error"}, {"type": "turn.completed"}], "reply"),
            (["broken JSON", {"type": "turn.completed"}], "reply"),
            ([{"type": "item.completed", "item": []}, {"type": "turn.completed"}], "reply"),
            ([{"type": "turn.completed"}, {"type": "turn.started"}], "old reply"),
        ]:
            with self.subTest(stream=stream):
                self.assertFalse(self.inspect(stream, response)[0]["complete"])


class DriverTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name).resolve()
        (self.path / "bin/lib").mkdir(parents=True)
        for name in ("codex-run", "codex-fanout"):
            shutil.copy2(ROOT / "bin" / name, self.path / "bin" / name)
        shutil.copy2(ROOT / "bin/lib/codex-events.py", self.path / "bin/lib/codex-events.py")
        # Timeout lifecycle has its own existing suite. This fixture forwards argv only.
        (self.path / "bin/lib/runtimeout.sh").write_text('runtimeout_resolve() { RUNTIMEOUT="$1/bin/runtimeout"; }\n')
        self.executable("runtimeout", '#!/bin/sh\nshift\nexec "$@"\n')
        self.executable("codex", '''#!/usr/bin/env python3
import json,sys
from pathlib import Path
args=sys.argv[1:]
out=Path(args[args.index('-o')+1])
Path('calls.jsonl').open('a').write(json.dumps(args)+'\\n')
if '--json' not in args:
    out.write_text('legacy body')
    print('legacy stdout')
    sys.exit(0)
prompt=next((a for a in args if a.startswith('CASE_')), '')
print(json.dumps({'type':'thread.started','thread_id':'fixture-id'}))
if prompt == 'CASE_BAD_JSON':
    print('not JSON')
if prompt != 'CASE_EMPTY':
    print(json.dumps({'type':'item.completed','item':{'type':'agent_message','text':'event body'}}))
if prompt != 'CASE_NO_OUTPUT_FILE':
    out.write_text('final body' if prompt != 'CASE_EMPTY' else '')
if prompt != 'CASE_PARTIAL':
    print(json.dumps({'type':'turn.completed','usage':{'input_tokens':7}}))
if prompt == 'CASE_FAIL':
    print(json.dumps({'type':'turn.failed','error':{'message':'failure'}}))
    sys.exit(9)
''')
        self.env = dict(os.environ, PATH=str(self.path / "bin") + os.pathsep + os.environ["PATH"])
        self.runner = self.path / "bin/codex-run"
        self.schema = self.path / "schema.json"
        self.schema.write_text('{"type":"object"}')

    def executable(self, name, content):
        path = self.path / "bin" / name
        path.write_text(content)
        path.chmod(0o755)

    def invoke(self, case, mode="ro", options=()):
        prompt = self.path / "prompt.md"
        prompt.write_text(case)
        return subprocess.run([str(self.runner), *options, "-o", str(self.path / "out"), mode, str(prompt)],
                              cwd=self.path, env=self.env, capture_output=True, text=True)

    def test_legacy_invocation_and_log_stay_compatible(self):
        result = self.invoke("CASE_OK")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.path / "out/ro.out.md").read_text(), "legacy body")
        self.assertFalse((self.path / "out/ro.events.jsonl").exists())

    def test_json_invocation_records_metadata_and_readable_log(self):
        result = self.invoke("CASE_OK", options=("-J",))
        self.assertEqual(result.returncode, 0, result.stderr)
        meta = json.loads((self.path / "out/ro.meta.json").read_text())
        self.assertTrue(meta["complete"])
        self.assertEqual(meta["thread_id"], "fixture-id")
        self.assertIn("final body", (self.path / "out/ro.log").read_text())

    def test_partial_and_invalid_json_reject_cli_success(self):
        for case in ("CASE_PARTIAL", "CASE_BAD_JSON", "CASE_EMPTY"):
            with self.subTest(case=case):
                shutil.rmtree(self.path / "out", ignore_errors=True)
                result = self.invoke(case, options=("-J",))
                self.assertEqual(result.returncode, 1, result.stderr)
                self.assertEqual((self.path / "out/ro.cli.rc").read_text().strip(), "0")
                self.assertEqual((self.path / "out/ro.rc").read_text().strip(), "65")

    def test_cli_failure_keeps_original_exit_code(self):
        result = self.invoke("CASE_FAIL", options=("-J",))
        self.assertEqual(result.returncode, 1, result.stderr)
        self.assertEqual((self.path / "out/ro.rc").read_text().strip(), "9")

    def test_review_event_fallback_keeps_body_in_log(self):
        result = self.invoke("CASE_NO_OUTPUT_FILE", mode="review", options=("-J",))
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("event body", (self.path / "out/review.log").read_text())

    def test_schema_absolute_path_survives_working_directory_change(self):
        worktree = self.path / "worktree"
        worktree.mkdir()
        result = self.invoke("CASE_OK", options=("-J", "-S", "schema.json", "-C", str(worktree)))
        self.assertEqual(result.returncode, 0, result.stderr)
        args = json.loads((worktree / "calls.jsonl").read_text())
        self.assertEqual(args[args.index('--output-schema')+1], str(self.schema))

    def test_schema_review_mode_is_rejected_before_cli_start(self):
        result = self.invoke("CASE_OK", mode="review", options=("-S", str(self.schema)))
        self.assertEqual(result.returncode, 1)
        self.assertFalse((self.path / "calls.jsonl").exists())

    def test_partial_fanout_excludes_incomplete_run_from_merger(self):
        ok = self.path / "ok.md"; ok.write_text("CASE_OK")
        partial = self.path / "partial.md"; partial.write_text("CASE_PARTIAL")
        merger = self.path / "merger.md"; merger.write_text("merge")
        manifest = self.path / "manifest.tsv"
        manifest.write_text(f"ok\tro\tm1\thigh\t{ok}\nbad\tro\tm1\thigh\t{partial}\n")
        result = subprocess.run([str(self.path / "bin/codex-fanout"), "-J", "-m", str(merger), str(manifest), str(self.path / "out")],
                                cwd=self.path, env=self.env, capture_output=True, text=True)
        self.assertEqual(result.returncode, 2, result.stderr)
        prompt = (self.path / "out/merger.prompt.md").read_text()
        self.assertIn("bad:rc=65", prompt)
        self.assertNotIn("bad.out.md", prompt)
        self.assertTrue((self.path / "out/digest.md").is_file())


if __name__ == "__main__":
    unittest.main()
