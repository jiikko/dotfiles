"""lint の群 G13 (pause_comma) の本家の関数に入力を通して、結果を JSON で返す driver (lint/pausecomma の正解)。

標準入力: {"scripts_dir": "...", "texts": [文字列, ...], "docs": [Markdown の文字列, ...], "files": [{"path": 入力の集まりのパス, "data": 中身}, ...]}
  文字列は、単独のサロゲートを含むなら {"ws": [cp, ...]}
標準出力: {"texts": [str の結果, ...], "docs": [表の組, ...], "files": [{"path", "lines": [str の結果, ...], "table", "table_objects"}, ...]}
  str の結果は {"line" か "text", "hits": _pause_comma_hits, "objects": _pause_comma_object_hits}。当たりは [位置, かたまり, 句]
  表は _pause_comma_line_hits(analyze_markdown(text), objects) の {"keys": [表にある行番号...], "hits": [[行番号, [当たり...]], ...]}
入力の集まりのファイルは lint と同じく decode_source(bytes)[0] を読み、行 (str の 2 関数) は "\\n" で割った行のうち trigger の字を含むものだけを返す。
"""
import json
import sys

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])
import markdown_visibility  # noqa: E402
import yomiyasu_lint as lint  # noqa: E402

TRIGGERS = ("は、", "が、", "も、", "を、", "を，", "を,")


def dec(v):
    if isinstance(v, dict):
        return "".join(map(chr, v["ws"]))
    return v


def enc(s):
    return {"ws": [ord(c) for c in s]} if any(0xD800 <= ord(c) <= 0xDFFF for c in s) else s


def hits(items):
    return [[i, enc(chunk), enc(phrase)] for i, chunk, phrase in items]


def str_case(text):
    return {"hits": hits(lint._pause_comma_hits(text)), "objects": hits(lint._pause_comma_object_hits(text))}


def table(text, objects):
    out = lint._pause_comma_line_hits(markdown_visibility.analyze_markdown(text), objects=objects)
    return {"keys": sorted(out), "hits": [[line, hits(out[line])] for line in sorted(out) if out[line]]}


def tables(text):
    return {"table": table(text, False), "table_objects": table(text, True)}


texts = []
for raw in plan["texts"]:
    case = str_case(dec(raw))
    case["text"] = raw
    texts.append(case)
docs = []
for raw in plan["docs"]:
    entry = tables(dec(raw))
    entry["text"] = raw
    docs.append(entry)
files = []
for f in plan["files"]:
    text = markdown_visibility.decode_source(f["data"].encode("utf-8"))[0]
    lines = []
    for k, line in enumerate(text.split("\n")):
        if any(t in line for t in TRIGGERS):
            case = str_case(line)
            case["line"] = k
            lines.append(case)
    entry = {"path": f["path"], "lines": lines}
    entry.update(tables(text))
    files.append(entry)
json.dump({"texts": texts, "docs": docs, "files": files}, sys.stdout, ensure_ascii=True, separators=(",", ":"))
