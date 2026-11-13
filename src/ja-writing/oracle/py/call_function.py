"""本家の module を import して関数に JSON の引数を渡し、戻り値を JSON で返す driver。

標準入力: {"scripts_dir": "...", "calls": [{"module": "...", "function": "...", "args": [...], "kwargs": {...}}, ...],
          "git": 絶対パス (op git で使う), "git_env": {...} (op git の環境変数。空から作る)}
標準出力: [{"result": <JSON>} または {"exception": {"type": "...", "message": "..."}}, ...] (calls と同じ順)
JSON に直せない戻り値 (set・bytes など) は exception の type を "Unserializable" にして、戻り値の型の名前を message に入れる。

1074 で足したもの (どれも足さなければ、前と一字も同じ出力):
- "digest": true の呼び出しは、結果 (か例外) を正規の JSON (sort_keys・ensure_ascii・separators=(",", ":")・re.Pattern は {"flags", "pattern"}) にして
  SHA-256 の先頭 8 字を "digest" に入れ、例外だったときは "raised": true も足す。"keep": true も付けたときだけ、普通の result / exception も添える (大きな結果を記録に入れない)。
  fixture の操作 ("op") が例外になったときは keep が無くても exception を添える (呼び出し側が、どの操作がなぜ失敗したかを出して止まるため)。
- "get": true の呼び出しは関数を呼ばずに、module の属性 (定数) そのものを返す。
- "save": "名前" の呼び出しは、戻り値の Python の値を覚える。args の中の {"$ref": "名前"} が、その値になる (JSON を経由しない)。
- args の中の {"$bytes_b64": "..."} は bytes、{"$str": [コードポイント...]} は str (単独のサロゲートも持てる)。
- 文字列の中の "<<ROOT>>" は実行の作業フォルダ (cwd の実パス) に置き換える。結果の文字列の中の作業フォルダは "<<ROOT>>" に戻す。
- "quiet": true の呼び出し ("digest" も付ける) は、結果の digest を 8 つの 0 にする。覚える値 (save) を作るだけで、結果そのもの (大きな解析の結果など) の記録は要らない呼び出し用。例外になったときは普通の digest と raised。
- "op" の呼び出しは、作業フォルダに fixture を作る・環境を変える: write・mkdir・symlink・chdir・chmod・remove・setenv・unsetenv・restoreenv・git。結果は null。
"""
import base64
import hashlib
import json
import os
import re
import shutil
import subprocess
import sys

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])

ROOT = os.getcwd()
ROOT_TOKEN = "<<ROOT>>"
registry = {}
saved_env = {}


def resolve(value):
    """args の中の印と <<ROOT>> を実際の値にする。"""
    if isinstance(value, dict):
        if len(value) == 1:
            (key, inner), = value.items()
            if key == "$ref":
                return registry[inner]
            if key == "$bytes_b64":
                return base64.b64decode(inner)
            if key == "$str":
                return "".join(chr(cp) for cp in inner)
        return {k: resolve(v) for k, v in value.items()}
    if isinstance(value, list):
        return [resolve(v) for v in value]
    if isinstance(value, str):
        return value.replace(ROOT_TOKEN, ROOT)
    return value


def unroot(value):
    """結果の文字列の中の作業フォルダを <<ROOT>> に戻す。"""
    if isinstance(value, dict):
        return {k: unroot(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [unroot(v) for v in value]
    if isinstance(value, str):
        return value.replace(ROOT, ROOT_TOKEN)
    return value


def encode_default(value):
    # 集合は並びが決まらないので昇順の配列にする (要素は str のものだけが対象。要素の比べは Go 側が同じ昇順で作る)
    if isinstance(value, (set, frozenset)):
        return sorted(value)
    if isinstance(value, re.Pattern):
        return {"pattern": value.pattern, "flags": value.flags}
    # 本家の _LazyPattern (初めて使うときに組む正規表現。pattern は組まずに読め、flags を読むと組む)
    if type(value).__name__ == "_LazyPattern":
        return {"pattern": value.pattern, "flags": value.flags}
    raise TypeError(type(value).__name__)


def canonical(record):
    return json.dumps(record, sort_keys=True, ensure_ascii=True, separators=(",", ":"), default=encode_default)


def remember_env(name):
    if name not in saved_env:
        saved_env[name] = os.environ.get(name)


def run_op(op):
    kind = op["op"]
    path = resolve(op.get("path", ""))
    if kind == "write":
        os.makedirs(os.path.dirname(path), exist_ok=True)
        if "bytes_b64" in op:
            data = base64.b64decode(op["bytes_b64"]).replace(ROOT_TOKEN.encode("ascii"), os.fsencode(ROOT))
        else:
            data = resolve(op.get("content", "")).encode("utf-8", "surrogatepass")
        with open(path, "wb") as handle:
            handle.write(data)
    elif kind == "mkdir":
        os.makedirs(path, exist_ok=True)
    elif kind == "symlink":
        os.makedirs(os.path.dirname(path), exist_ok=True)
        os.symlink(resolve(op["target"]), path)
    elif kind == "chdir":
        os.chdir(path)
    elif kind == "chmod":
        os.chmod(path, op["mode"])
    elif kind == "remove":
        if os.path.isdir(path) and not os.path.islink(path):
            shutil.rmtree(path)
        else:
            os.remove(path)
    elif kind == "setenv":
        remember_env(op["name"])
        os.environ[op["name"]] = resolve(op["value"])
    elif kind == "unsetenv":
        remember_env(op["name"])
        os.environ.pop(op["name"], None)
    elif kind == "restoreenv":
        for name, old in saved_env.items():
            if old is None:
                os.environ.pop(name, None)
            else:
                os.environ[name] = old
        saved_env.clear()
        os.chdir(ROOT)
    elif kind == "git":
        env = {k: resolve(v) for k, v in plan["git_env"].items()}
        done = subprocess.run([plan["git"]] + [resolve(a) for a in op["args"]], cwd=path or None, env=env, capture_output=True, check=False)
        if done.returncode != 0:
            raise RuntimeError("git %s が rc=%d: %s" % (" ".join(op["args"]), done.returncode, done.stderr.decode("utf-8", "replace")))
    else:
        raise ValueError("未知の op: " + kind)
    return None


out = []
for call in plan["calls"]:
    try:
        if "op" in call:
            value = run_op(call["op"])
        else:
            module = __import__(call["module"])
            if call.get("get"):
                value = getattr(module, call["function"])
            else:
                value = getattr(module, call["function"])(*resolve(call.get("args", [])), **resolve(call.get("kwargs", {})))
        if call.get("save"):
            registry[call["save"]] = value
        try:
            json.dumps(value, ensure_ascii=True, default=encode_default)
        except (TypeError, ValueError) as exc:
            record = {"exception": {"type": "Unserializable", "message": type(value).__name__ + ": " + str(exc)}}
        else:
            record = {"result": unroot(value)}
    except Exception as exc:  # noqa: BLE001 - 例外も記録する (本家が投げる例外は仕様の一部)
        record = {"exception": {"type": type(exc).__name__, "message": unroot(str(exc))}}
    if call.get("digest"):
        if call.get("quiet") and "exception" not in record:
            entry = {"digest": "00000000"}
        else:
            entry = {"digest": hashlib.sha256(canonical(record).encode("ascii")).hexdigest()[:8]}
        if "exception" in record:
            entry["raised"] = True
        if call.get("keep") or ("op" in call and "exception" in record):
            entry.update(record)
        out.append(entry)
    else:
        out.append(record)
json.dump(out, sys.stdout, ensure_ascii=True, separators=(",", ":"), default=encode_default)
