"""本家の module を import して関数に JSON の引数を渡し、戻り値を JSON で返す driver。

標準入力: {"scripts_dir": "...", "calls": [{"module": "...", "function": "...", "args": [...], "kwargs": {...}}, ...]}
標準出力: [{"result": <JSON>} または {"exception": {"type": "...", "message": "..."}}, ...] (calls と同じ順)
JSON に直せない戻り値 (set・bytes など) は exception の type を "Unserializable" にして、戻り値の型の名前を message に入れる。
"""
import json
import sys

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])

out = []
for call in plan["calls"]:
    try:
        module = __import__(call["module"])
        value = getattr(module, call["function"])(*call.get("args", []), **call.get("kwargs", {}))
        try:
            json.dumps(value, ensure_ascii=True)
        except (TypeError, ValueError) as exc:
            out.append({"exception": {"type": "Unserializable", "message": type(value).__name__ + ": " + str(exc)}})
            continue
        out.append({"result": value})
    except Exception as exc:  # noqa: BLE001 - 例外も記録する (本家が投げる例外は仕様の一部)
        out.append({"exception": {"type": type(exc).__name__, "message": str(exc)}})
json.dump(out, sys.stdout, ensure_ascii=True, separators=(",", ":"))
