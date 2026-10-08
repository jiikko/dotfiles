"""Inspect a Codex JSONL run. Preserve observations without asserting code correctness."""
import argparse
import json
from pathlib import Path


def inspect_events(events_path, response_path):
    thread_id = None
    completed = False
    errors = []
    usages = []
    commands = []
    messages = []
    parse_errors = []
    for line_no, line in enumerate(events_path.read_text().splitlines(), 1):
        if not line.strip():
            continue
        try:
            event = json.loads(line)
            if not isinstance(event, dict):
                raise ValueError("event must be an object")
        except (ValueError, TypeError) as exc:
            parse_errors.append({"line": line_no, "error": str(exc)})
            continue
        kind = event.get("type")
        if kind == "thread.started":
            thread_id = event.get("thread_id")
        elif kind == "turn.started":
            completed = False
        elif kind == "turn.completed":
            completed = True
            usages.append(event.get("usage", {}))
        elif kind in ("turn.failed", "error"):
            errors.append(event)
        elif kind == "item.completed":
            item = event.get("item", {})
            if not isinstance(item, dict):
                parse_errors.append({"line": line_no, "error": "item must be an object"})
                continue
            if item.get("type") == "agent_message" and isinstance(item.get("text"), str):
                messages.append(item["text"])
            elif item.get("type") == "command_execution":
                commands.append({key: item.get(key) for key in
                                 ("id", "command", "exit_code", "status")})
    response = response_path.read_text() if response_path.is_file() else ""
    source = "output_last_message"
    if not response.strip() and messages:
        response = messages[-1]
        source = "agent_message_event"
    metadata = {
        "thread_id": thread_id, "turn_completed": completed,
        "errors": errors, "parse_errors": parse_errors, "usage": usages,
        "commands": commands, "response_source": source if response.strip() else None,
        "response_present": bool(response.strip()),
    }
    valid = completed and bool(response.strip()) and not errors and not parse_errors
    metadata["complete"] = valid
    return metadata, response


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("events", type=Path)
    parser.add_argument("response", type=Path)
    parser.add_argument("metadata", type=Path)
    parser.add_argument("log", type=Path)
    args = parser.parse_args()
    try:
        metadata, response = inspect_events(args.events, args.response)
    except (OSError, UnicodeError) as exc:
        metadata, response = {"complete": False, "read_error": str(exc)}, ""
    args.metadata.write_text(json.dumps(metadata, ensure_ascii=False, indent=2) + "\n")
    # Keep the legacy textual log readable for merger and users; raw JSONL is separate.
    with args.log.open("a") as log:
        log.write("\n--- Codex JSONL result ---\n")
        if response.strip():
            log.write(response + "\n")
        if not metadata["complete"]:
            log.write("Incomplete JSONL run; see " + str(args.metadata) + "\n")
    return 0 if metadata["complete"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
