"""本家の module を import して関数に JSON の引数を渡し、戻り値を JSON で返す driver。

標準入力: {"scripts_dir": "...", "calls": [{"module": "...", "function": "...", "args": [...], "kwargs": {...}}, ...],
          "git": 絶対パス (op git で使う), "git_env": {...} (op git の環境変数。空から作る)}
標準出力: [{"result": <JSON>} または {"exception": {"type": "...", "message": "..."}}, ...] (calls と同じ順)
JSON に直せない戻り値 (set・bytes など) は exception の type を "Unserializable" にして、戻り値の型の名前を message に入れる。

1074 で足したもの (どれも足さなければ、前と一字も同じ出力):
- "digest": true の呼び出しは、結果 (か例外) を正規の JSON (sort_keys・ensure_ascii・separators=(",", ":")・re.Pattern は {"flags", "pattern"}) にして
  SHA-256 の先頭 8 字を "digest" に入れ、例外だったときは "raised": true も足す。"keep": true も付けたときだけ、普通の result / exception も添える (大きな結果を記録に入れない)。
  fixture の操作 ("op") が例外になったときは keep が無くても exception を添える (呼び出し側が、どの操作がなぜ失敗したかを出して止まるため)。
- "args_out_only": [添字...] の呼び出しは、結果を [戻り値, 呼んだ後の args のうち、その添字のもの] にする (args に解析の結果のような JSON にできない値があるときに、書き足される引数だけを記録する)。
  args の中の {"$ref": "名前", "path": [...]} は、覚えた値の添字・鍵を辿った一部 (_build_full_diff の戻り値の tuple から _Sides や hunks を取る)。
  args の中の {"$concat": [...]} は、要素 (list か {"$ref"}) をつないだ 1 つの list。{"$column": 値, "n": 添字} は、値 (list) の各行の n 番目を集めた list
  (_number_risks の戻り値 (番号, 順, risk) の並びから risk の並びを取る)。どちらも足すだけで、使わなければ前と一字も同じ出力。
- "method": "名前" の呼び出しは、関数 (クラスでもよい) を呼んだ戻り値の、その名前のメソッドを引数なしで呼んだ結果を戻り値にする
  (difflib のように、組み立てた照合器の get_opcodes の結果を覚えて次の呼び出しへ渡す。付けなければ前と一字も同じ出力)。
- "args_out": true の呼び出しは、結果を [戻り値, 呼んだ後の args] にする (引数の list に書き足す関数 (calque の型の判定が語を足す) の足した中身を記録する。付けなければ前と一字も同じ出力)。
- "get": true の呼び出しは関数を呼ばずに、module の属性 (定数) そのものを返す。
- "save": "名前" の呼び出しは、戻り値の Python の値を覚える。args の中の {"$ref": "名前"} が、その値になる (JSON を経由しない)。
- args の中の {"$bytes_b64": "..."} は bytes、{"$str": [コードポイント...]} は str (単独のサロゲートも持てる)。
- 文字列の中の "<<ROOT>>" は実行の作業フォルダ (cwd の実パス) に置き換える。結果の文字列の中の作業フォルダは "<<ROOT>>" に戻す。
- "quiet": true の呼び出し ("digest" も付ける) は、結果の digest を 8 つの 0 にする。覚える値 (save) を作るだけで、結果そのもの (大きな解析の結果など) の記録は要らない呼び出し用。例外になったときは普通の digest と raised。
- "folded": true の呼び出しは、結果の文字列の中の「作業フォルダを NFC にして casefold したもの」を "<<ROOT>>" に戻す (ふつうの呼び出しは作業フォルダそのものを戻す)。
  大文字と小文字を区別しない FS で、パスの比較用の綴り (casefold したもの) を返す関数の結果が、作業フォルダの綴りの大小に依らず同じ記録になるようにする。
  付けなければ前と一字も同じ出力。
- "op" の呼び出しは、作業フォルダに fixture を作る・環境を変える: write・mkdir・symlink・hardlink・mkfifo・chdir・chmod・remove・setenv・unsetenv・restoreenv・git。結果は null。
  op tree だけは結果を返す: path から下の木 ([相対パス, 種類, mode, 中身] の並び。下の tree の docstring)。状態フォルダへの書き込みの結果を比べる用。
- "save_raised": true の呼び出しは、例外になったときも、その例外を save の名前で覚える (書けなかった理由の文を作る _write_problem に渡す)。
- "temporaries": true の呼び出しは、結果と例外の文の中の一時ファイルの名前 (.tmp-<16 桁の 16 進>) を ".tmp-*" にする (名前は乱数で、実行ごとに変わる)。
  どの 3 つも、付けなければ前と一字も同じ出力。
- args の中の {"$namespace": {...}} は argparse.Namespace (中の値も同じ印を解く)。op stdin は sys.stdin を bytes_b64 (か content の UTF-8) を読む
  テキストの入力 (buffer を持つ) にする。name が "none" なら sys.stdin を None にする。
- "objects": true の呼び出しは、結果の中の本家の _Input を {"record": record(), "label", "text"}、_Target を as_json()、bytes を {"bytes_b64": ...} にする
  (入力の読みの関数の戻り値を記録する用)。どれも、使わなければ前と一字も同じ出力。
- check の verify の記録用 (使わなければ前と一字も同じ出力):
  - "capture": true の呼び出しは、標準出力と標準エラーを取り込み、結果を {"rc", "stdout", "stderr"} にする (CLI の main を呼ぶ用)。
    標準エラーの「yomiyasu_check.py: 内部エラー: 」で始まる行から後ろ (例外の型と文とトレースバック。トレースバックは置き場のパスを含む) は
    「yomiyasu_check.py: 内部エラー」の 1 行にする。
  - op clock は本家の check の時刻 (_utc_now) を value の ISO 8601 に固定する。初めての op clock か op pycli で、check の
    time.monotonic を定数にし (記録の duration_ms を 0 にする)、新しい実行 ID を「時刻-<8 桁の連番>」にする (乱数を使わない)。
  - op pycli は本家の check の main(args) を出力を捨てて呼ぶ。終了コードが value (「,」区切り) に無ければ止める。呼んだ後、
    作業フォルダの下の記録の最終稿のパスと標準入力の鍵から、by-path の鍵と _place の文字列の対応を覚える (op snapshot が使う)。
  - op pyedit は path の JSON のファイルを value の指示 (JSON) で書き換える: set ([鍵の道, 値] の並び)・delete (鍵の道の並び)・
    rehash (記録の checks のレポートと本文の SHA-256 をファイルから求め直す)・redigest (record_digest を求め直す)・keep_mtime (書き換えた後に
    mtime を書き換える前の値に戻す。set などと併せる)・raw ([元, 後] の
    字の置き換え。JSON として読まずに書き換える)・copy_from (そのパスのファイルを、バイト列のまま path に写す。ほかの指示とは併せない)。
  - op rename は value の {"globs": [path からの相対の glob...], "old", "new"} で、当たったファイルの名前の最初の old を new に替える
    (mtime はそのまま。当たらない glob と、名前に old を持たないファイルは止める)。記録の回の番号を移す用。
  - op snapshot は path から下の木を返す: {"entries": [[相対パス, 種類, mode, mtime_ns, 中身の鍵か先, 付け足し]...], "blobs": {鍵: zlib で縮めた中身の base64}}。
    by-path の鍵のうち作業フォルダのパスから作ったものは、印にした _place の文字列の SHA-256 を名前にする。返す前に、通常のファイルの mtime を、元の順を保った決まった値 (2026-01-01 から 1 ミリ秒ずつ) にそろえる。
    value が空でなければ {"ties": [[fnmatch のパターン...]...]} で、組ごとに、どれかのパターンに当たる通常のファイル (path からの相対パス。by-path の鍵は元の名前) の mtime を
    組の中で最も早い値にそろえる (mtime が 1 秒単位のファイルシステムで同じ秒に書いた場面。当たるファイルが無いパターンは止める)。中身の作業フォルダは
    <<ROOT>> にし、記録の中の、作業フォルダのパスを中身に持つファイル (語の一覧のパスを書いたレポート) の SHA-256 は
    <<SHA:そのファイルの相対パス>> にし、記録の record_digest は、中身と合っていたものを "sha256:<<DIGEST>>"、合っていなかったものを "sha256:" と 0 の 64 字にする。
    並びは相対パスの順。付け足しは、記録のファイルでは record_digest が中身と合っていたか、index/by-path/ の下のフォルダでは鍵の元の
    _place の文字列 (作業フォルダを NFC にして casefold したものを <<ROOT>> にしたもの)。blobs は同じ path の前の snapshot で返した鍵を省く。
- check の run の記録用 (使わなければ前と一字も同じ出力):
  - capture で本家の check の main を呼んだ後も、op pycli と同じく by-path の鍵の元を覚える (run が書いた木を op snapshot に通すため。
    作業フォルダの外の状態フォルダ (--state-dir ../) にも書くので、実行の作業フォルダ全体から探す)。
  - op pyexe は、本家の check が lint と diff の子に使う sys.executable を path (子の代わりの shell script) にし、本物の python3 の
    パスを環境変数 JA_REAL に入れる。path が空なら sys.executable を戻す (JA_REAL は restoreenv で戻る)。
"""
import argparse
import base64
import fnmatch
import glob
import io
import hashlib
import json
import os
import re
import shutil
import stat
import subprocess
import sys
import tempfile
import unicodedata
import zlib

plan = json.loads(sys.stdin.read())
sys.path.insert(0, plan["scripts_dir"])

ROOT = os.getcwd()
ROOT_TOKEN = "<<ROOT>>"
ROOT_FOLDED = unicodedata.normalize("NFC", ROOT).casefold()
registry = {}
saved_env = {}


def resolve(value):
    """args の中の印と <<ROOT>> を実際の値にする。"""
    if isinstance(value, dict):
        if set(value) == {"$ref", "path"}:
            # 覚えた値の一部 (tuple・list の添字、dict の鍵を辿る)
            picked = registry[value["$ref"]]
            for step in value["path"]:
                picked = picked[step]
            return picked
        if set(value) == {"$column", "n"}:
            return [row[value["n"]] for row in resolve(value["$column"])]
        if len(value) == 1:
            (key, inner), = value.items()
            if key == "$ref":
                return registry[inner]
            if key == "$concat":
                joined = []
                for part in resolve(inner):
                    joined += part
                return joined
            if key == "$bytes_b64":
                return base64.b64decode(inner)
            if key == "$str":
                return "".join(chr(cp) for cp in inner)
            if key == "$namespace":
                return argparse.Namespace(**resolve(inner))
        return {k: resolve(v) for k, v in value.items()}
    if isinstance(value, list):
        return [resolve(v) for v in value]
    if isinstance(value, str):
        return value.replace(ROOT_TOKEN, ROOT)
    return value


def unroot(value, root=None):
    """結果の文字列の中の作業フォルダ (folded の呼び出しでは、それを NFC にして casefold したもの) を <<ROOT>> に戻す。"""
    root = ROOT if root is None else root
    if isinstance(value, dict):
        return {k: unroot(v, root) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [unroot(v, root) for v in value]
    if isinstance(value, str):
        return value.replace(root, ROOT_TOKEN)
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


TEMPORARY = re.compile(r"\.tmp-[0-9a-f]{16}")


def mask_temporaries(value):
    if isinstance(value, dict):
        return {k: mask_temporaries(v) for k, v in value.items()}
    if isinstance(value, list):
        return [mask_temporaries(v) for v in value]
    if isinstance(value, str):
        return TEMPORARY.sub(".tmp-*", value)
    return value


def tree(top):
    """top から下の木。[相対パス (top は "."), 種類 (dir・file・link・other・missing), mode (S_IMODE。missing は errno), 中身] の並び。
    中身は file なら UTF-8 の文字列、link なら先、読めないフォルダは一覧を読めなかった errno、ほかは null。リンクは辿らない。"""
    entries = []

    def visit(path, rel):
        try:
            info = os.lstat(path)
        except OSError as exc:
            entries.append([rel, "missing", exc.errno, None])
            return
        mode = stat.S_IMODE(info.st_mode)
        if stat.S_ISLNK(info.st_mode):
            entries.append([rel, "link", mode, os.readlink(path)])
        elif stat.S_ISDIR(info.st_mode):
            try:
                names = sorted(os.listdir(path))
            except OSError as exc:
                entries.append([rel, "dir", mode, exc.errno])
                return
            entries.append([rel, "dir", mode, None])
            for name in names:
                visit(os.path.join(path, name), name if rel == "." else rel + "/" + name)
        elif stat.S_ISREG(info.st_mode):
            with open(path, "rb") as handle:
                entries.append([rel, "file", mode, handle.read().decode("utf-8")])
        else:
            entries.append([rel, "other", mode, None])

    visit(top, ".")
    return entries


def plain_objects(value):
    """objects の呼び出しの結果を JSON にできる形にする (driver の冒頭の objects)。"""
    if isinstance(value, (list, tuple)):
        return [plain_objects(v) for v in value]
    if isinstance(value, bytes):
        return {"bytes_b64": base64.b64encode(value).decode("ascii")}
    if type(value).__name__ == "_Input":
        return {"record": value.record(), "label": value.label, "text": value.text}
    if type(value).__name__ == "_Target":
        return value.as_json()
    return value


INTERNAL_ERROR = "yomiyasu_check.py: 内部エラー"
check_patched = []
real_executable = []
check_clock = ["2026-01-01T00:00:00Z"]
check_places = {}
snapshot_blobs = {}
SNAPSHOT_BASE_NS = 1767225600 * 1000000000
DIGEST_TOKEN = "sha256:<<DIGEST>>"
DIGEST_WRONG = "sha256:" + "0" * 64


def check_module():
    """本家の check を import し、初めてのときだけ時刻・monotonic・実行 ID を決まった値にする (op clock と pycli の説明)。"""
    module = __import__("yomiyasu_check")
    if not check_patched:
        import datetime

        class FixedTime(object):
            def __getattr__(self, name):
                import time as real
                return getattr(real, name)

            @staticmethod
            def monotonic():
                return 1000.0

        counter = [0]

        def new_run_id(state, moment):
            counter[0] += 1
            return module._stamp(moment) + "-%08x" % counter[0]

        module._utc_now = lambda: datetime.datetime.strptime(check_clock[0], "%Y-%m-%dT%H:%M:%SZ").replace(
            tzinfo=datetime.timezone.utc)
        module.time = FixedTime()
        module._new_run_id = new_run_id
        check_patched.append(True)
    return module


def capture_main(function, args):
    """CLI の main を、標準出力と標準エラーを取り込んで呼ぶ。{"rc", "stdout", "stderr"}。"""
    saved = sys.stdout, sys.stderr
    out, err = io.BytesIO(), io.BytesIO()
    # 包みを変数に持つ: 参照が切れた TextIOWrapper は下の BytesIO を閉じ、getvalue が読めなくなる
    wrapped = io.TextIOWrapper(out, encoding="utf-8", newline="\n"), io.TextIOWrapper(err, encoding="utf-8", newline="\n")
    sys.stdout, sys.stderr = wrapped
    try:
        try:
            rc = function(*args)
        except SystemExit as exc:
            # argparse の誤りと -h は main を通らずに SystemExit で終わる (python3 の終了コードはその code)
            rc = 0 if exc.code is None else exc.code
    finally:
        for stream in wrapped:
            try:
                stream.flush()
            except (OSError, ValueError):
                pass
        sys.stdout, sys.stderr = saved
    stdout, stderr = out.getvalue().decode("utf-8"), err.getvalue().decode("utf-8")
    del wrapped
    at = stderr.find(INTERNAL_ERROR + ": ")
    if at >= 0 and (at == 0 or stderr[at - 1] == "\n"):
        stderr = stderr[:at] + INTERNAL_ERROR + "\n"
    return {"rc": rc, "stdout": stdout, "stderr": stderr}


def remember_places(module, start=None):
    """start (省くと作業フォルダ) の下の記録から、by-path の鍵とその元の文字列を覚える (op pycli の説明)。"""
    for top, _, names in os.walk(start or os.getcwd()):
        if os.path.basename(os.path.dirname(top)) != "runs":
            continue
        for name in names:
            if not re.match(r"^attempt-[0-9]+\.json$", name):
                continue
            record = module._read_record(os.path.join(top, name))
            inputs = record.get("inputs") if isinstance(record, dict) else None
            if not isinstance(inputs, dict):
                continue
            bases = []
            final = inputs.get("final")
            if isinstance(final, dict) and isinstance(final.get("path"), str):
                bases.append(module._place(final["path"]))
            source = inputs.get("source")
            if isinstance(source, dict) and isinstance(source.get("text_sha256"), str):
                bases.append("stdin:" + source["text_sha256"])
            materials = inputs.get("materials")
            if isinstance(materials, list) and all(isinstance(m, dict) and isinstance(m.get("text_sha256"), str) for m in materials):
                bases.append("stdin:" + "".join(sorted(m["text_sha256"] for m in materials)))
            for base in bases:
                check_places[module._sha256(base.encode("utf-8", "surrogateescape"))] = base.replace(ROOT_FOLDED, ROOT_TOKEN)


def edit_json(path, spec):
    """op pyedit (driver の冒頭)。"""
    module = check_module()
    if "copy_from" in spec:
        shutil.copyfile(resolve(spec["copy_from"]), path)
        return
    if "raw" in spec:
        with open(path, "rb") as handle:
            data = handle.read().decode("utf-8")
        for old, new in spec["raw"]:
            if old not in data:
                raise ValueError("pyedit raw: %r が %s にありません" % (old, path))
            data = data.replace(old, new)
        with open(path, "wb") as handle:
            handle.write(data.encode("utf-8"))
        return
    with open(path, "rb") as handle:
        value = json.loads(handle.read().decode("utf-8"))

    def walk(steps):
        node = value
        for step in steps[:-1]:
            node = node[step]
        return node, steps[-1]

    for steps, new in spec.get("set", []):
        node, last = walk(steps)
        if isinstance(node, list) and last == len(node):
            node.append(new)
        else:
            node[last] = new
    for steps in spec.get("delete", []):
        node, last = walk(steps)
        del node[last]
    if spec.get("rehash"):
        state = os.path.dirname(os.path.dirname(os.path.dirname(path)))
        for entry in value["checks"]:
            for key, digest in (("report", "report_sha256"), ("text", "text_sha256")):
                if isinstance(entry.get(key), str):
                    with open(os.path.join(state, *entry[key].split("/")), "rb") as handle:
                        entry[digest] = module._sha256(handle.read())
    if spec.get("redigest"):
        value["record_digest"] = module._record_digest(value)
    before = os.stat(path).st_mtime_ns
    with open(path, "wb") as handle:
        handle.write((json.dumps(value, ensure_ascii=True, indent=1) + "\n").encode("ascii"))
    if spec.get("keep_mtime"):
        os.utime(path, ns=(before, before))


def snapshot(top, ties):
    """op snapshot (driver の冒頭)。"""
    module = check_module()
    files = []
    for folder, dirs, names in os.walk(top):
        dirs.sort()
        for name in sorted(names):
            path = os.path.join(folder, name)
            info = os.lstat(path)
            if stat.S_ISREG(info.st_mode):
                files.append((info.st_mtime_ns, path))
    stamps = {}
    for rank, (_, path) in enumerate(sorted(files)):
        stamps[path] = SNAPSHOT_BASE_NS + rank * 1000000
    for group in ties:
        members = []
        for pattern in group:
            hit = [path for _, path in files if fnmatch.fnmatchcase(os.path.relpath(path, top), pattern)]
            if not hit:
                raise ValueError("snapshot の ties のパターン %s に当たるファイルがありません" % pattern)
            members.extend(hit)
        first = min(stamps[path] for path in members)
        for path in members:
            stamps[path] = first
    for path, stamp in stamps.items():
        os.utime(path, ns=(stamp, stamp), follow_symlinks=False)
    seen = snapshot_blobs.setdefault(top, set())
    entries, blobs, pending = [], {}, []

    def visit(path, rel):
        info = os.lstat(path)
        mode = stat.S_IMODE(info.st_mode)
        if stat.S_ISLNK(info.st_mode):
            entries.append([rel, "link", mode, 0, os.readlink(path).replace(ROOT, ROOT_TOKEN), None])
        elif stat.S_ISDIR(info.st_mode):
            extra = None
            if os.path.basename(os.path.dirname(path)) == "by-path" and os.path.basename(os.path.dirname(os.path.dirname(path))) == "index":
                if os.path.basename(path) not in check_places:
                    raise ValueError("by-path の鍵 %s の元が分かりません" % os.path.basename(path))
                extra = check_places[os.path.basename(path)]
            if extra is not None and ROOT_TOKEN in extra:
                # 鍵は作業フォルダのパスで変わるので、印にした文字列の SHA-256 を名前にする (Go のテストが自分のパスで鍵を作り直す)
                rel = rel[:-len(os.path.basename(path))] + hashlib.sha256(extra.encode("utf-8", "surrogateescape")).hexdigest()
            if rel != ".":
                entries.append([rel, "dir", mode, 0, None, extra])
            restore = None
            if not mode & stat.S_IRUSR or not mode & stat.S_IXUSR:
                restore = mode
                os.chmod(path, mode | stat.S_IRUSR | stat.S_IXUSR)
            try:
                for name in sorted(os.listdir(path)):
                    visit(os.path.join(path, name), name if rel == "." else rel + "/" + name)
            finally:
                if restore is not None:
                    os.chmod(path, restore)
        elif stat.S_ISREG(info.st_mode):
            restore = None
            if not mode & stat.S_IRUSR:
                restore = mode
                os.chmod(path, mode | stat.S_IRUSR)
            try:
                with open(path, "rb") as handle:
                    data = handle.read()
            finally:
                if restore is not None:
                    os.chmod(path, restore)
            pending.append((rel, path, mode, info.st_mtime_ns, data))
        else:
            raise ValueError("snapshot は通常のファイル・フォルダ・リンクだけを写せます: " + path)

    visit(top, ".")
    # 作業フォルダのパスを中身に持つファイル (語の一覧のパスを書いたレポートなど) の SHA-256 は、記録の中で <<SHA:相対パス>> にする
    hashes = dict((hashlib.sha256(data).hexdigest(), "<<SHA:%s>>" % rel) for rel, _, _, _, data in pending
                  if os.fsencode(ROOT) in data and not is_record(rel))
    for rel, path, mode, mtime, data in pending:
        extra = None
        if is_record(rel):
            record = module._read_record(path)
            digest = record.get("record_digest") if isinstance(record, dict) else None
            extra = isinstance(digest, str) and digest == module._record_digest(record)
            if isinstance(digest, str) and re.match(r"^sha256:[0-9a-f]{64}$", digest):
                # digest は作業フォルダのパスを含む中身から作るので、合っていたものは印に (Go のテストが求め直す)、
                # 合っていなかったものは決まった合わない値にする
                data = data.replace(digest.encode("ascii"), (DIGEST_TOKEN if extra else DIGEST_WRONG).encode("ascii"))
            elif not isinstance(record, dict):
                # JSON として読めない記録も、digest の字だけは作業フォルダのパスで変わる
                data = re.sub(rb'("record_digest"[^"]*)"sha256:[0-9a-f]{64}"', rb'\1"' + DIGEST_WRONG.encode("ascii") + rb'"', data)
            for real, token in hashes.items():
                data = data.replace(real.encode("ascii"), token.encode("utf-8"))
        data = data.replace(os.fsencode(ROOT), ROOT_TOKEN.encode("ascii"))
        key = hashlib.sha256(data).hexdigest()[:16]
        if key not in seen:
            seen.add(key)
            blobs[key] = base64.b64encode(zlib.compress(data, 9)).decode("ascii")
        entries.append([rel, "file", mode, mtime, key, extra])
    entries.sort(key=lambda entry: entry[0])
    return {"entries": entries, "blobs": blobs}


def is_record(rel):
    """snapshot の相対パスが記録 (runs/<実行 ID>/attempt-<n>.json) か。"""
    parts = rel.split("/")
    return len(parts) >= 3 and parts[-3] == "runs" and re.match(r"^attempt-[0-9]+\.json$", parts[-1]) is not None


def remember_env(name):
    if name not in saved_env:
        saved_env[name] = os.environ.get(name)
    if name in ("TMPDIR", "TEMP", "TMP"):
        # tempfile.gettempdir はプロセスの中で 1 度だけ候補を試して覚える。case が変えた値で試し直させる
        # (本家の check は 1 回の起動で 1 度しか呼ばないので、起動ごとに試すのと同じになる)
        tempfile.tempdir = None


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
    elif kind == "hardlink":
        os.makedirs(os.path.dirname(path), exist_ok=True)
        os.link(resolve(op["target"]), path)
    elif kind == "mkfifo":
        os.makedirs(os.path.dirname(path), exist_ok=True)
        os.mkfifo(path, 0o644)
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
        if any(name in ("TMPDIR", "TEMP", "TMP") for name in saved_env):
            tempfile.tempdir = None
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
    elif kind == "tree":
        return tree(path)
    elif kind == "stdin":
        if op.get("name") == "none":
            sys.stdin = None
        else:
            data = base64.b64decode(op["bytes_b64"]) if "bytes_b64" in op else op.get("content", "").encode("utf-8", "surrogatepass")
            sys.stdin = io.TextIOWrapper(io.BytesIO(data), encoding="utf-8")
    elif kind == "clock":
        check_module()
        check_clock[0] = op["value"]
    elif kind == "pycli":
        module = check_module()
        result = capture_main(module.main, [[resolve(a) for a in op["args"]]])
        if str(result["rc"]) not in op["value"].split(","):
            raise RuntimeError("pycli %s が rc=%s: %s%s" % (" ".join(op["args"]), result["rc"], result["stderr"], result["stdout"][:400]))
        remember_places(module)
    elif kind == "rename":
        spec = json.loads(op["value"])
        for pattern in spec["globs"]:
            hits = sorted(glob.glob(os.path.join(glob.escape(path), pattern)))
            if not hits:
                raise ValueError("rename: %s に %s が当たりません" % (path, pattern))
            for hit in hits:
                base = os.path.basename(hit)
                if spec["old"] not in base:
                    raise ValueError("rename: %r が %s にありません" % (spec["old"], hit))
                os.rename(hit, os.path.join(os.path.dirname(hit), base.replace(spec["old"], spec["new"], 1)))
    elif kind == "pyedit":
        edit_json(path, json.loads(op["value"]))
    elif kind == "snapshot":
        return snapshot(path, json.loads(op["value"])["ties"] if op.get("value") else [])
    elif kind == "pyexe":
        if not real_executable:
            real_executable.append(sys.executable)
        remember_env("JA_REAL")
        if path:
            os.environ["JA_REAL"] = real_executable[0]
            sys.executable = path
        else:
            sys.executable = real_executable[0]
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
                args = resolve(call.get("args", []))
                if call.get("capture"):
                    value = capture_main(getattr(check_module() if call["module"] == "yomiyasu_check" else module, call["function"]), args)
                    if call["module"] == "yomiyasu_check":
                        remember_places(check_module(), ROOT)
                else:
                    value = getattr(module, call["function"])(*args, **resolve(call.get("kwargs", {})))
                if call.get("method"):
                    value = getattr(value, call["method"])()
                if call.get("objects"):
                    value = plain_objects(value)
                if call.get("args_out"):
                    value = [value, args]
                elif call.get("args_out_only") is not None:
                    value = [value, [args[i] for i in call["args_out_only"]]]
        if call.get("save"):
            registry[call["save"]] = value
        try:
            if not call.get("quiet"):
                json.dumps(value, ensure_ascii=True, default=encode_default)
        except (TypeError, ValueError) as exc:
            record = {"exception": {"type": "Unserializable", "message": type(value).__name__ + ": " + str(exc)}}
        else:
            record = {"result": unroot(value, ROOT_FOLDED if call.get("folded") else None)}
    except Exception as exc:  # noqa: BLE001 - 例外も記録する (本家が投げる例外は仕様の一部)
        if call.get("save_raised"):
            registry[call["save"]] = exc
        record = {"exception": {"type": type(exc).__name__, "message": unroot(str(exc))}}
    if call.get("temporaries"):
        record = mask_temporaries(record)
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
