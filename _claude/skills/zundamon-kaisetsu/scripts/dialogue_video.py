#!/usr/bin/env python3
"""VOICEVOX で台本を合成し、音声を埋め込んだ 1 ファイルの HTML プレイヤーを作る。

使い方は同じディレクトリの ../SKILL.md。標準ライブラリだけで動く (音声の圧縮に ffmpeg か afconvert を使う)。

  dialogue_video.py check                          # 必要なコマンドとエンジンの状態を確かめる
  dialogue_video.py up / down                      # エンジンをコンテナで起動 / 停止 (container を優先、無ければ docker)
  dialogue_video.py speakers                       # 話者とスタイル ID の一覧
  dialogue_video.py kana "文" … / kana --script script.json  # 文か台本の全行の読み (合成はしない)
  dialogue_video.py synth  script.json             # セリフごとに wav を作る (キャッシュあり)
  dialogue_video.py build  script.json -o out --format html|mp4|both  # 連結・口パク・HTML / mp4 化

HTML は音声 1 本 + タイムライン JSON をブラウザで再生し、字幕・立ち絵・口パクを audio.currentTime から毎フレーム描く
(シークしても音と絵がずれない)。mp4 はそのプレイヤーの描画モードを Chrome で撮って並べる。
"""

from __future__ import annotations

import argparse
import base64
import concurrent.futures
import hashlib
import io
import json
import math
import mimetypes
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import wave
from pathlib import Path

SKILL_DIR = Path(__file__).resolve().parent.parent
TEMPLATE = SKILL_DIR / "templates" / "player.html"
# 立ち絵の既定の置き場 (skill のディレクトリからの相対)。素材の再配布になるので公開リポジトリには置かない。
# dotfiles では非公開リポジトリをここにサブモジュールとして置いている。それ以外の環境は README の Setup で用意する
ASSETS_FACES = SKILL_DIR / "assets" / "zundamon-kaisetsu" / "faces"
DEFAULT_ENGINE = os.environ.get("VOICEVOX_URL", "http://127.0.0.1:50021")
IMAGE = "voicevox/voicevox_engine:cpu-latest"
# ユーザーが自分で立てた同名のコンテナを up / down で巻き込まないよう、この skill 専用の名前にする
CONTAINER_NAME = "zundamon-kaisetsu-voicevox"
START_HINT = f"  起動: python3 {Path(__file__).resolve()} up   (container か docker でエンジンを立てる)"
SAMPLE_RATE = 24000  # VOICEVOX の既定出力。全セリフをこの値に揃えて連結する
MOUTH_FPS = 30
VIDEO_SIZE = (1280, 720)  # mp4 は 720p 固定 (字幕・アバターの大きさはこの解像度で合わせている)
# mp4 の撮影前に、CPU が混んでいたら空くまで待つ。負荷が高いときに headless Chrome が watchdog で落ちた
# (rc=2 "own watchdog expired"。同じ条件の再実行で通った) ので、負荷との関係を仮説として入れている。
# 1 分平均の load がコア数 × LOAD_BUSY_RATIO 以上を「混んでいる」とし、LOAD_WAIT_MAX 秒待っても空かなければそのまま撮る
LOAD_BUSY_RATIO = 0.8
LOAD_WAIT_MAX = 20 * 60
LOAD_POLL = 15
SHEET_STATES = 20  # まとめ撮り 1 回の枚数。縦 720 x 20 = 14400px (Chrome が 1 枚で撮れる高さに収める)
# キャラクターは四国めたんとずんだもんに固定する。名前・既定の声・色・立ち位置の正本はここ。
# mirror は立ち絵を左右反転して表示する。2 人が向き合うよう、素材の向き (めたんは画面の左向き、
# ずんだもんは左寄り向き) と立ち位置から決めている。素材を替えて向きが変わったら見直す
CAST = {
    "metan": {"name": "四国めたん", "style_id": 2, "color": "#d9418c", "side": "left", "mirror": True},
    "zundamon": {"name": "ずんだもん", "style_id": 3, "color": "#2e9e3a", "side": "right", "mirror": False},
}
CAST_OPTIONS = ("faces", "style_id", "speed", "pitch", "intonation", "volume")  # 台本の cast.<キャラ> で変えてよいもの
# 表情の語彙。faces/*.json (psd_faces.py の定義) はキャラごとにこの全部を定義する
FACES = ("通常", "笑顔", "説明", "驚き", "困り", "考え中", "怒り", "悲しみ")
DEFAULT_FACE = "通常"

# 母音ごとの口の開き (0 閉じ / 1 半開き / 2 開き)。大文字 (無声化母音) は表に無いので 0 = ほぼ音が出ない
VOWEL_MOUTH = {"a": 2, "o": 2, "i": 1, "u": 1, "e": 1, "N": 0, "cl": 0, "pau": 0}
LIP_CLOSED_CONSONANTS = {"m", "my", "b", "by", "p", "py"}


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(1)


# ---------- engine ----------

def engine_request(engine: str, path: str, params: dict | None = None, body: bytes | None = None) -> bytes:
    url = engine.rstrip("/") + path
    if params:
        url += "?" + urllib.parse.urlencode(params)
    req = urllib.request.Request(url, data=body, method="GET" if body is None else "POST")
    if body is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=120) as res:
            return res.read()
    except urllib.error.HTTPError as e:
        die(f"{path} が HTTP {e.code} を返した: {e.read().decode('utf-8', 'replace')[:300]}")
    except (urllib.error.URLError, OSError) as e:  # 接続拒否・起動途中の切断・timeout はどれも OSError の仲間
        die(f"VOICEVOX エンジン ({engine}) と通信できない ({path}): {getattr(e, 'reason', e)}\n{START_HINT}")
    raise AssertionError("unreachable")


def engine_version(engine: str) -> str | None:
    try:
        with urllib.request.urlopen(engine.rstrip("/") + "/version", timeout=3) as res:
            return res.read().decode().strip().strip('"')
    except (urllib.error.URLError, OSError, ValueError):
        return None


# ---------- runtime (container / docker) ----------

def detect_runtime() -> str | None:
    """Apple の container CLI を優先し、無ければ docker。どちらも無ければ None (デスクトップアプリで代用する)。"""
    for name in ("container", "docker"):
        if shutil.which(name):
            return name
    return None


def run_quiet(cmd: list[str], timeout: float = 60) -> tuple[int, str]:
    try:
        r = subprocess.run(cmd, capture_output=True, text=True, stdin=subprocess.DEVNULL, timeout=timeout)
        return r.returncode, (r.stdout + r.stderr).strip()
    except subprocess.TimeoutExpired:
        return 124, f"{timeout:.0f} 秒で応答しない"
    except OSError as e:
        return 127, str(e)


def runtime_service(rt: str) -> tuple[bool, str]:
    """ランタイムのサービスが動いているか。動いていなければ起こし方を返す。"""
    if rt == "container":
        rc, out = run_quiet(["container", "system", "status"])
        return rc == 0, ("container system start   (初回はカーネルを入れるか聞かれる。"
                         "非対話なら --enable-kernel-install を付ける)" if rc else out.splitlines()[0] if out else "")
    rc, out = run_quiet(["docker", "info"])
    return rc == 0, ("Docker Desktop (か colima 等) を起動する" if rc else "")


def local_port(engine: str) -> int:
    u = urllib.parse.urlparse(engine)
    if u.hostname not in ("127.0.0.1", "localhost"):
        die(f"up / down は手元のエンジンにだけ使える ({engine} は別ホスト)")
    return u.port or 80


def cmd_check(args: argparse.Namespace) -> None:
    ok = True

    def row(good: bool, label: str, detail: str) -> None:
        print(f"{'OK' if good else 'NG'}  {label}: {detail}")

    row(sys.version_info >= (3, 9), "python", sys.version.split()[0] + ("" if sys.version_info >= (3, 9) else " (3.9 以上が要る)"))
    ok &= sys.version_info >= (3, 9)
    enc = "ffmpeg" if shutil.which("ffmpeg") else "afconvert" if shutil.which("afconvert") else None
    row(enc is not None, "音声の圧縮", enc or "ffmpeg も afconvert も無い (brew install ffmpeg)")
    ok &= enc is not None

    uv = shutil.which("uv")
    row(uv is not None, "立ち絵の書き出し: uv", uv or "無い (psd_faces.py で PSD から表情を書き出すときだけ要る。brew install uv)")
    chrome = find_chrome()
    row(chrome is not None, "mp4 用: Chrome", chrome or "無い (mp4 を作るときだけ要る。環境変数 CHROME で指定できる)")
    row(h264_encoder() is not None, "mp4 用: H.264", "ffmpeg で書ける" if h264_encoder() else "ffmpeg が無いか H.264 を書けない (mp4 を作るときだけ要る)")

    ver = engine_version(args.engine)
    rt = detect_runtime()
    if rt:
        running, hint = runtime_service(rt)
        row(True, "コンテナ", rt + (" (container を優先)" if rt == "container" else " (container が無いので docker)"))
        row(running or ver is not None, f"{rt} のサービス", "起動中" if running else f"停止中 → {hint}")
    else:
        row(ver is not None, "コンテナ", "container も docker も無い → VOICEVOX のデスクトップアプリを起動すれば同じエンジンが使える")
    row(ver is not None, "VOICEVOX エンジン", f"{args.engine} で応答 (版 {ver})" if ver else f"{args.engine} で応答なし → up で起動する")
    ok &= ver is not None
    if not ok:
        sys.exit(1)


def cmd_up(args: argparse.Namespace) -> None:
    port = local_port(args.engine)
    ver = engine_version(args.engine)
    if ver:
        print(f"up: {args.engine} は既に応答している (版 {ver})。起動はしない", file=sys.stderr)
        return
    rt = detect_runtime()
    if not rt:
        die("container も docker も無い。VOICEVOX のデスクトップアプリを起動するか、どちらかを入れる")
    running, hint = runtime_service(rt)
    if not running:
        die(f"{rt} のサービスが動いていない → {hint}")
    cmd = [rt, "run", "--rm", "-d", "-p", f"127.0.0.1:{port}:50021", "--name", CONTAINER_NAME, IMAGE]
    print(f"up: {' '.join(cmd)}   (初回はイメージの取得に数分かかる)", file=sys.stderr)
    r = subprocess.run(cmd, stdin=subprocess.DEVNULL)  # 取得の進捗を見せるため出力は端末へ流す
    if r.returncode != 0:
        die(f"{rt} run が失敗 (rc={r.returncode})。同名のコンテナが残っているなら down してから再実行する")
    for _ in range(90):  # 起動直後は十数秒応答しない
        ver = engine_version(args.engine)
        if ver:
            print(f"up: 起動した ({rt}, 版 {ver})。終わったら down で止める", file=sys.stderr)
            return
        time.sleep(2)
    die(f"180 秒待っても {args.engine} が応答しない。{rt} logs {CONTAINER_NAME} で原因を見て、"
        f"やり直す前に down で止める (コンテナは起動したまま残っている)")


def cmd_down(args: argparse.Namespace) -> None:
    # up 以降に container を入れた・消した場合でも取り残さないよう、優先順位ではなく両方のランタイムを見る
    rts = [rt for rt in ("container", "docker") if shutil.which(rt)]
    if not rts:
        die("container も docker も無い (デスクトップアプリならアプリを終了する)")
    stopped = []
    for rt in rts:
        if not runtime_service(rt)[0]:
            continue  # サービスが止まっていれば、そのランタイムのコンテナも動いていない
        rc, out = run_quiet([rt, "stop", CONTAINER_NAME], timeout=120)
        if rc == 0:
            stopped.append(rt)
        elif not re.search(r"not ?found|no such container", out, re.I):
            die(f"{rt} stop {CONTAINER_NAME} が失敗 (rc={rc}): {out[:300]}")
    print(f"down: {CONTAINER_NAME} を止めた ({', '.join(stopped)})" if stopped
          else f"down: {CONTAINER_NAME} は動いていない", file=sys.stderr)
    ver = engine_version(args.engine)
    if ver:
        print(f"down: {args.engine} はまだ応答している (版 {ver})。この skill 以外 (デスクトップアプリ等) のエンジン", file=sys.stderr)


def fetch_styles(engine: str) -> dict[int, str]:
    """スタイル ID → 話者名。"""
    return {st["id"]: sp["name"] for sp in json.loads(engine_request(engine, "/speakers")) for st in sp["styles"]}


def cmd_kana(args: argparse.Namespace) -> None:
    """文ごとに audio_query の読み (kana) を出す。read の候補 (カタカナ・ひらがな・英字のまま) を合成せずに比べる用。

    --script を付けると、台本の全行について 行番号 / 話者 / 字幕 / 読み を出す。synth 済みの行はキャッシュの読みを使い、
    無い行だけエンジンに問い合わせる (合成はしない)。音声を差し替えた行 (read か readings) には * を付ける。
    """
    if args.script:
        if args.texts:
            die("kana は --script か文のどちらか一方を渡す")
        script_path = Path(args.script).resolve()
        script = load_script(script_path)
        wd = work_dir(script_path)
        for i, line in enumerate(script["lines"]):
            p = line_params(script, line)
            query_path = cache_paths(wd, p)[1]
            if query_path.exists():
                kana = json.loads(query_path.read_text(encoding="utf-8")).get("kana", "")
            else:
                kana = json.loads(engine_request(args.engine, "/audio_query",
                                                 {"text": p["text"], "speaker": p["style_id"]}, b"")).get("kana", "")
            mark = "*" if p["text"] != line["text"] else " "
            print(f"{i}\t{line['who']}\t{mark}{line['text']}\t{kana}")
        return
    if not args.texts:
        die("kana には読みを見たい文か --script <台本> を渡す")
    sid = args.style_id if args.style_id is not None else CAST[args.who]["style_id"]
    for text in args.texts:
        kana = json.loads(engine_request(args.engine, "/audio_query", {"text": text, "speaker": sid}, b"")).get("kana", "")
        print(f"{text}\t{kana}")


def cmd_speakers(args: argparse.Namespace) -> None:
    for sp in json.loads(engine_request(args.engine, "/speakers")):
        styles = ", ".join(f"{st['name']}={st['id']}" for st in sp["styles"])
        print(f"{sp['name']}: {styles}")


# ---------- script ----------

def load_script(path: Path) -> dict:
    try:
        script = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as e:
        die(f"{path}: 読めない ({e})")
    cast = script.get("cast") or {}
    lines = script.get("lines")
    if not isinstance(cast, dict) or not set(cast) <= set(CAST):
        die(f"{path}: cast に書けるのは {'/'.join(CAST)} だけ (キャラクターは固定)")
    if not isinstance(lines, list) or not lines:
        die(f"{path}: lines が空")
    for key in ("lead_in", "gap"):
        if key in script:
            nonneg(path, key, script[key])
    readings = script.get("readings", {})
    if not isinstance(readings, dict) or not all(
            isinstance(k, str) and k and isinstance(v, str) and v.strip() for k, v in readings.items()):
        die(f"{path}: readings は {{\"字幕の語\": \"読ませたい語\"}} の形 (キーも値も空でない文字列)")
    for name, c in cast.items():
        extra = set(c) - set(CAST_OPTIONS)
        if extra:
            die(f"{path}: cast.{name} に書けるのは {'/'.join(CAST_OPTIONS)} だけ (実際: {sorted(extra)})")
    # 固定の設定に台本の上書きを重ねる。以降の処理は script["cast"] だけを見る
    script["cast"] = {k: {**CAST[k], **cast.get(k, {})} for k in CAST}
    faces = {k: face_list(path, k, c) for k, c in script["cast"].items()}
    for i, line in enumerate(lines):
        who = line.get("who")
        if who not in CAST:
            die(f"{path}: lines[{i}].who は {'/'.join(CAST)} のどれか (実際: {who!r})")
        face = line.get("face", DEFAULT_FACE)
        if face not in FACES:
            die(f"{path}: lines[{i}].face は {'/'.join(FACES)} のどれか (実際: {face!r})")
        if faces[who] is not None and face not in faces[who]:
            die(f"{path}: lines[{i}].face={face!r} が cast.{who}.faces の書き出しに無い (psd_faces.py の定義に足す)")
        if not str(line.get("text", "")).strip():
            die(f"{path}: lines[{i}].text が空")
        if "read" in line and not str(line["read"]).strip():
            die(f"{path}: lines[{i}].read が空 (読みを直さないなら read を書かない)")
        if "pause_after" in line:
            nonneg(path, f"lines[{i}].pause_after", line["pause_after"])
        try:
            p = line_params(script, line)
        except (TypeError, ValueError) as e:
            die(f"{path}: lines[{i}] の数値が不正 ({e})")
        if p["speed"] <= 0:
            die(f"{path}: lines[{i}] の speed は正の数")
    return script


def faces_dir(script_path: Path, key: str, cast: dict) -> Path | None:
    """立ち絵の dir。台本の cast.<キャラ>.faces が無ければ、skill の assets/ にあれば使う。"""
    if cast.get("faces"):
        return (script_path.parent / Path(cast["faces"]).expanduser()).resolve()
    default = ASSETS_FACES / key
    return default if (default / "faces.json").is_file() else None


def face_list(script_path: Path, key: str, cast: dict) -> list[str] | None:
    """cast.<キャラ>.faces (psd_faces.py の出力 dir) にある表情の一覧。faces が無ければ None (丸アバターで出す)。"""
    d = faces_dir(script_path, key, cast)
    if d is None:
        return None
    try:
        names = json.loads((d / "faces.json").read_text(encoding="utf-8"))["faces"]
    except (OSError, json.JSONDecodeError, KeyError) as e:
        die(f"cast.{key}.faces: {d}/faces.json が読めない ({e})。psd_faces.py で書き出した dir を指定する")
    return names


def nonneg(path: Path, key: str, v) -> None:
    if not isinstance(v, (int, float)) or isinstance(v, bool) or v < 0:
        die(f"{path}: {key} は 0 以上の数 (実際: {v!r})")


def work_dir(script_path: Path) -> Path:
    return script_path.parent / (script_path.stem + ".work")


def spoken_text(script: dict, line: dict) -> str:
    """音声にする文。字幕は text のまま。行の read があればそれを使い、無ければ text に台本全体の readings を当てる。

    readings は 1 回の走査で置き換える。同じ位置では長いキーを優先し (「Hash」と「HashMap」の両方があれば HashMap)、
    置き換えた結果をもう一度置き換えない (語ごとに replace を重ねると、結果に別のキーが含まれたとき二重に変わる)。
    """
    if "read" in line:
        return line["read"]
    readings = script.get("readings", {})
    if not readings:
        return line["text"]
    pattern = re.compile("|".join(map(re.escape, sorted(readings, key=len, reverse=True))))
    return pattern.sub(lambda m: readings[m.group(0)], line["text"])


def line_params(script: dict, line: dict) -> dict:
    """合成結果を決める入力をすべて集める。キャッシュの鍵もここから作るので、合成に効く値を足したらここに足す。"""
    cast = script["cast"][line["who"]]
    return {
        "text": spoken_text(script, line),
        "style_id": int(line.get("style_id", cast["style_id"])),
        "speed": float(line.get("speed", cast.get("speed", script.get("speed", 1.0)))),
        "pitch": float(line.get("pitch", cast.get("pitch", 0.0))),
        "intonation": float(line.get("intonation", cast.get("intonation", 1.0))),
        "volume": float(line.get("volume", cast.get("volume", 1.0))),
    }


def cache_paths(wd: Path, params: dict) -> tuple[Path, Path]:
    """鍵は合成入力だけで決める (行番号を含めない)。行を挿入・削除しても、他の行は合成し直さずに済む。

    エンジンの版・ユーザー辞書は鍵に入らない (build のたびにエンジンへ問い合わせずに済ませるため)。
    それらを変えたら synth --force で作り直す。
    """
    key = hashlib.sha256(json.dumps(params, sort_keys=True, ensure_ascii=False).encode()).hexdigest()[:16]
    return wd / f"{key}.wav", wd / f"{key}.query.json"


def check_wav_bytes(data: bytes, what: str) -> None:
    """mono / 16bit / SAMPLE_RATE で、ヘッダの長さどおりに PCM が入っているかを確かめる。"""
    try:
        with wave.open(io.BytesIO(data), "rb") as w:
            fmt = (w.getnchannels(), w.getsampwidth(), w.getframerate())
            n = w.getnframes()
            got = len(w.readframes(n))
    except (wave.Error, EOFError) as e:
        die(f"{what}: wav として読めない ({e})")
    if fmt != (1, 2, SAMPLE_RATE):
        die(f"{what}: mono / 16bit / {SAMPLE_RATE}Hz ではない ({fmt[0]}ch, {fmt[1] * 8}bit, {fmt[2]}Hz)")
    if n == 0 or got != n * 2:
        die(f"{what}: wav が空か途中で切れている (ヘッダ {n} フレーム / 実データ {got // 2} フレーム)")


def cmd_synth(args: argparse.Namespace) -> None:
    script_path = Path(args.script).resolve()
    script = load_script(script_path)
    styles = fetch_styles(args.engine)
    for i, line in enumerate(script["lines"]):
        sid = line_params(script, line)["style_id"]
        if sid not in styles:
            die(f"lines[{i}]: style_id={sid} がエンジンに無い (speakers サブコマンドで調べる)")
        want = script["cast"][line["who"]]["name"]
        if styles[sid] != want:
            die(f"lines[{i}]: style_id={sid} は {styles[sid]} の声で、{want} の声ではない")

    wd = work_dir(script_path)
    wd.mkdir(exist_ok=True)
    made = cached = 0
    for i, line in enumerate(script["lines"]):
        p = line_params(script, line)
        wav_path, query_path = cache_paths(wd, p)
        if wav_path.exists() and query_path.exists() and not args.force:
            cached += 1
            continue
        query = json.loads(engine_request(args.engine, "/audio_query", {"text": p["text"], "speaker": p["style_id"]}, b""))
        query.update(speedScale=p["speed"], pitchScale=p["pitch"], intonationScale=p["intonation"],
                     volumeScale=p["volume"], outputSamplingRate=SAMPLE_RATE, outputStereo=False)
        wav = engine_request(args.engine, "/synthesis", {"speaker": p["style_id"]}, json.dumps(query).encode())
        check_wav_bytes(wav, f"lines[{i}] の合成結果")  # 壊れた wav をキャッシュに残さない
        # 書きかけのファイルをキャッシュとして拾わないよう、一時名に書いてから rename する
        for path, data in ((query_path, json.dumps(query, ensure_ascii=False).encode()), (wav_path, wav)):
            tmp = path.with_suffix(path.suffix + ".part")
            tmp.write_bytes(data)
            tmp.replace(path)
        made += 1
        print(f"[{i + 1}/{len(script['lines'])}] {line['who']}: {p['text'][:30]}", file=sys.stderr)
    keep = {q.name for line in script["lines"] for q in cache_paths(wd, line_params(script, line))}
    unused = [f for f in wd.iterdir() if f.name not in keep]
    print(f"synth: 合成 {made} / キャッシュ {cached} / 計 {len(script['lines'])} → {wd}"
          + (f" (台本から外れた古いファイル {len(unused)} 件。{wd.name}/ ごと消して synth し直してもよい)" if unused else ""),
          file=sys.stderr)


# ---------- build ----------

def read_wav(path: Path) -> tuple[bytes, float]:
    data = path.read_bytes()
    check_wav_bytes(data, f"{path} (synth --force で作り直す)")
    with wave.open(io.BytesIO(data), "rb") as w:
        n = w.getnframes()
        return w.readframes(n), n / SAMPLE_RATE


def mouth_track(query: dict, duration: float) -> str:
    """audio_query のモーラ長から、MOUTH_FPS ごとの口の開き (0/1/2) を並べた文字列を作る。

    モーラ長は speedScale で割った値が実際の発話長になる。前後の無音 (pre/postPhonemeLength) を含めた
    合計が wav の長さと食い違う分は、全体を線形に伸縮して吸収する (エンジンの版で前後無音の扱いが違っても
    口が音より先行・遅延し続けないようにするため)。
    """
    speed = query.get("speedScale", 1.0) or 1.0
    segs: list[tuple[float, int]] = [(query.get("prePhonemeLength", 0.1), 0)]
    for ap in query.get("accent_phrases", []):
        for mora in ap.get("moras", []):
            cons_len = (mora.get("consonant_length") or 0) / speed
            if cons_len:
                segs.append((cons_len, 0 if mora.get("consonant") in LIP_CLOSED_CONSONANTS else 1))
            segs.append(((mora.get("vowel_length") or 0) / speed, VOWEL_MOUTH.get(mora.get("vowel"), 0)))
        pm = ap.get("pause_mora")
        if pm:
            segs.append(((pm.get("vowel_length") or 0) / speed, 0))
    segs.append((query.get("postPhonemeLength", 0.1), 0))
    nominal = sum(d for d, _ in segs) or duration
    ratio = duration / nominal
    edges, acc = [], 0.0
    for d, lv in segs:
        acc += d * ratio
        edges.append((acc, lv))
    out, j = [], 0
    for k in range(max(1, round(duration * MOUTH_FPS))):
        t = (k + 0.5) / MOUTH_FPS
        while j < len(edges) - 1 and edges[j][0] <= t:
            j += 1
        out.append(str(edges[j][1]))
    return "".join(out)


def encode_audio(wav_path: Path, out_path: Path, kbps: int) -> None:
    if shutil.which("ffmpeg"):
        cmd = ["ffmpeg", "-v", "error", "-y", "-i", str(wav_path), "-ac", "1", "-c:a", "aac", "-b:a", f"{kbps}k",
               "-movflags", "+faststart", str(out_path)]
    elif shutil.which("afconvert"):
        cmd = ["afconvert", "-f", "m4af", "-d", "aac", "-b", str(kbps * 1000), str(wav_path), str(out_path)]
    else:
        die("ffmpeg も afconvert も無い (brew install ffmpeg)")
    r = subprocess.run(cmd, capture_output=True, text=True)
    if r.returncode != 0 or not out_path.exists() or out_path.stat().st_size == 0:
        die(f"音声の圧縮に失敗 ({cmd[0]}, rc={r.returncode}): {r.stderr.strip()[:300]}")


def data_uri(path: Path, mime: str | None = None) -> str:
    mime = mime or mimetypes.guess_type(path.name)[0] or "application/octet-stream"
    return f"data:{mime};base64," + base64.b64encode(path.read_bytes()).decode()


def load_faces(script_path: Path, script: dict) -> dict:
    """台本で使う表情の立ち絵を {キャラ: {表情: [閉じ, 半開き, 開き] の data URI}} にする。

    音声の圧縮より前に呼び、ファイルの欠けで長い処理を無駄にしない。使わない表情は埋め込まない (HTML が太るため)。
    """
    used = {k: {DEFAULT_FACE} for k in CAST}
    for line in script["lines"]:
        used[line["who"]].add(line.get("face", DEFAULT_FACE))
    out = {}
    for key, c in script["cast"].items():
        d = faces_dir(script_path, key, c)
        imgs = {}
        for face in (sorted(used[key]) if d else []):
            files = [d / f"{face}_{lv}.webp" for lv in range(3)]
            missing = [f.name for f in files if not f.is_file()]
            if missing:
                die(f"cast.{key}.faces: {d} に {', '.join(missing)} が無い (psd_faces.py で書き出し直す)")
            imgs[face] = [data_uri(f, "image/webp") for f in files]
        out[key] = imgs
    return out


def face_credits(script_path: Path, script: dict) -> list[str]:
    out = []
    for key, c in script["cast"].items():
        d = faces_dir(script_path, key, c)
        if d:
            credit = json.loads((d / "faces.json").read_text(encoding="utf-8")).get("credit")
            if credit and credit not in out:
                out.append(credit)
    return out


def line_faces(lines: list[dict]) -> list[dict]:
    """各行の時点で、それぞれのキャラが見せる表情。話す行は face (省略は通常)、話さない側は最後に話したときの表情のまま。"""
    cur, out = {k: DEFAULT_FACE for k in CAST}, []
    for line in lines:
        cur[line["who"]] = line.get("face", DEFAULT_FACE)
        out.append(dict(cur))
    return out


def frame_runs(timeline: list[dict], duration: float) -> list[list[int]]:
    """MOUTH_FPS ごとの見た目の状態を、変わり目だけの列 [開始フレーム, 字幕の行, 話し中か, 口の開き] にする。

    HTML プレイヤーの再生と mp4 の画像は、どちらもこの列から描く (状態の判定をここ 1 か所に置く)。
    字幕は、次の行が始まるまで直前の行を出し続ける。口が動くのはその行の発話中 (start <= t < end) だけ。
    """
    runs: list[list[int]] = []
    li = -1
    for k in range(max(1, math.ceil(duration * MOUTH_FPS))):
        t = (k + 0.5) / MOUTH_FPS
        while li + 1 < len(timeline) and timeline[li + 1]["start"] <= t:
            li += 1
        speaking, level = 0, 0
        if li >= 0 and t < timeline[li]["end"]:
            mouth = timeline[li]["mouth"]
            idx = int((t - timeline[li]["start"]) * MOUTH_FPS)
            speaking, level = 1, int(mouth[idx]) if idx < len(mouth) else 0
        if not runs or runs[-1][1:] != [li, speaking, level]:
            runs.append([k, li, speaking, level])
    return runs


def assemble(script_path: Path, script: dict, td: Path, kbps: int) -> tuple[dict, Path]:
    """合成済みの wav を連結して m4a を作り、プレイヤーに渡すデータ (音声以外) を組む。"""
    wd = work_dir(script_path)
    lead_in = float(script.get("lead_in", 0.4))
    default_gap = float(script.get("gap", 0.35))
    images = load_faces(script_path, script)
    faces = line_faces(script["lines"])

    pcm = bytearray(b"\x00\x00" * round(lead_in * SAMPLE_RATE))
    timeline, chapters, missing = [], [], []
    for i, line in enumerate(script["lines"]):
        wav_path, query_path = cache_paths(wd, line_params(script, line))
        if not (wav_path.exists() and query_path.exists()):
            missing.append(i)
            continue
        frames, dur = read_wav(wav_path)
        start = len(pcm) / 2 / SAMPLE_RATE
        pcm += frames
        if line.get("chapter"):
            chapters.append({"title": str(line["chapter"]), "start": round(start, 3)})
        timeline.append({
            "who": line["who"], "text": str(line["text"]), "faces": faces[i],
            "start": round(start, 3), "end": round(start + dur, 3),
            "mouth": mouth_track(json.loads(query_path.read_text(encoding="utf-8")), dur),
        })
        pcm += b"\x00\x00" * round(float(line.get("pause_after", default_gap)) * SAMPLE_RATE)
    if missing:
        die(f"合成済みの wav が無い行がある (台本を変えた後に synth していない?): lines {missing[:10]}")

    joined = td / "joined.wav"
    with wave.open(str(joined), "wb") as w:
        w.setnchannels(1)
        w.setsampwidth(2)
        w.setframerate(SAMPLE_RATE)
        w.writeframes(bytes(pcm))
    m4a = td / "audio.m4a"
    encode_audio(joined, m4a, kbps)

    duration = len(pcm) / 2 / SAMPLE_RATE
    data = {
        "title": str(script.get("title", script_path.stem)),
        "description": str(script.get("description", "")),
        # 声のクレジット (利用規約で必須) と立ち絵のクレジットは自動で入れる。台本の credits はその後に足す
        "credits": list(dict.fromkeys(
            [f"VOICEVOX:{CAST[k]['name']}" for k in CAST if any(l["who"] == k for l in script["lines"])]
            + face_credits(script_path, script) + [str(c) for c in script.get("credits", [])])),
        "cast": {key: {"name": c["name"], "color": c["color"], "side": c["side"], "mirror": c["mirror"], "images": images[key]}
                 for key, c in script["cast"].items()},
        "defaultFace": DEFAULT_FACE,
        "chapters": chapters,
        "lines": [{k: v for k, v in l.items() if k != "mouth"} for l in timeline],
        "frames": frame_runs(timeline, duration),
        "duration": round(duration, 3), "fps": MOUTH_FPS, "audio": "",
    }
    return data, m4a


def render_html(data: dict) -> str:
    # </script> で JSON が途切れないよう "<" をエスケープする (JSON としては同じ値)
    payload = json.dumps(data, ensure_ascii=False).replace("<", "\\u003c")
    title = data["title"].replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
    html = TEMPLATE.read_text(encoding="utf-8")
    if html.count("__DATA_JSON__") != 1 or html.count("__TITLE__") != 1:
        die(f"{TEMPLATE}: 差し込み位置 (__DATA_JSON__ / __TITLE__) がそれぞれ 1 つではない")
    # 1 回の走査で両方を差し込む (順に replace すると、先に入れた値の中の印まで置換してしまう)
    return re.sub(r"__DATA_JSON__|__TITLE__", lambda m: payload if m.group(0) == "__DATA_JSON__" else title, html)


def find_chrome() -> str | None:
    cands = [os.environ.get("CHROME"),
             "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
             "/Applications/Chromium.app/Contents/MacOS/Chromium"]
    cands += [shutil.which(n) for n in ("google-chrome", "google-chrome-stable", "chromium", "chromium-browser")]
    return next((c for c in cands if c and os.access(c, os.X_OK)), None)


def h264_encoder() -> list[str] | None:
    if not shutil.which("ffmpeg"):
        return None
    rc, out = run_quiet(["ffmpeg", "-hide_banner", "-encoders"])
    if rc == 0 and re.search(r"^\s*V\S*\s+libx264\s", out, re.M):
        return ["-c:v", "libx264", "-preset", "medium", "-crf", "20", "-tune", "stillimage"]
    if rc == 0 and re.search(r"^\s*V\S*\s+h264_videotoolbox\s", out, re.M):
        return ["-c:v", "h264_videotoolbox", "-b:v", "3M"]
    return None


def ffq(path: Path) -> str:
    """ffconcat の引用。パスに ' があっても壊れないよう '\\'' で閉じて開き直す。"""
    return "'" + str(path).replace("'", "'\\''") + "'"


def png_size(path: Path) -> tuple[int, int]:
    head = path.read_bytes()[:24]
    if head[:8] != b"\x89PNG\r\n\x1a\n":
        return 0, 0
    return int.from_bytes(head[16:20], "big"), int.from_bytes(head[20:24], "big")


def wait_for_idle_cpu() -> None:
    """CPU が混んでいる間、最大 LOAD_WAIT_MAX 秒待つ。待っている間も 1 分ごとに状況を出す (黙って止まって見えないように)。"""
    cores = os.cpu_count() or 1
    limit = cores * LOAD_BUSY_RATIO
    start = time.monotonic()
    last_note = None
    while True:
        load = os.getloadavg()[0]
        waited = time.monotonic() - start
        if load < limit:
            if last_note is not None:
                print(f"build: CPU が空いた (load {load:.1f} / {cores} コア、{waited / 60:.1f} 分待った)", file=sys.stderr)
            return
        if waited >= LOAD_WAIT_MAX:
            print(f"build: {LOAD_WAIT_MAX // 60} 分待っても CPU が空かない (load {load:.1f} / {cores} コア)。"
                  "そのまま撮る (Chrome が落ちたら空いてから build し直す)", file=sys.stderr)
            return
        if last_note is None or waited - last_note >= 60:
            print(f"build: CPU が混んでいる (load {load:.1f} / {cores} コア、閾値 {limit:.1f})。"
                  f"空くまで最大 {LOAD_WAIT_MAX // 60} 分待つ (経過 {waited / 60:.0f} 分)", file=sys.stderr)
            last_note = waited
        time.sleep(LOAD_POLL)


def write_mp4(data: dict, m4a: Path, out: Path, td: Path, jobs: int) -> None:
    """HTML プレイヤーで「見た目の状態」ごとの絵を Chrome に撮らせ、状態の列どおりに並べて音声と合わせる。

    見た目を HTML 版と同じにするため、絵は自前で描かずプレイヤーに描かせる。撮るのは見た目の状態の
    種類の数 (行数 × 口の段階 程度) だけで、フレームの数ではない。Chrome の起動 (1 回 1 秒強) が律速なので、
    プレイヤーのまとめ撮りモード (#sheet=) で SHEET_STATES 枚を縦に並べて 1 回で撮り、ffmpeg で切り分ける。
    """
    chrome = find_chrome()
    enc = h264_encoder()
    if not chrome:
        die("mp4 には Chrome か Chromium が要る (環境変数 CHROME で実行ファイルを指定できる)")
    if not enc:
        die("mp4 には H.264 を書ける ffmpeg が要る (brew install ffmpeg)")
    page = td / "render.html"
    page.write_text(render_html(data), encoding="utf-8")
    wait_for_idle_cpu()
    states = sorted({tuple(r[1:]) for r in data["frames"]})
    shots = {st: td / f"state_{st[0] + 1}_{st[1]}_{st[2]}.png" for st in states}
    groups = [states[i:i + SHEET_STATES] for i in range(0, len(states), SHEET_STATES)]
    w, h = VIDEO_SIZE

    def shoot(gi: int) -> str | None:
        """1 グループを撮って切り分ける。失敗したら理由を返す。"""
        group, sheet = groups[gi], td / f"sheet_{gi}.png"
        # --user-data-dir は付けない: 付けると撮影後も Chrome の更新プロセスが残って終了しない (実測)。
        # 付けなければ並列に起動しても衝突しない
        cmd = [chrome, "--headless=new", "--disable-gpu", "--hide-scrollbars", "--force-device-scale-factor=1",
               f"--window-size={w},{h * len(group)}", "--virtual-time-budget=3000", f"--screenshot={sheet}",
               page.as_uri() + "#sheet=" + ";".join(f"{a},{b},{c}" for a, b, c in group)]
        rc, msg = run_quiet(cmd, timeout=120)
        if rc != 0 or not sheet.is_file():
            return f"Chrome が失敗 (rc={rc}): {msg[-300:]}"
        if png_size(sheet) != (w, h * len(group)):  # 縦に長すぎて切られた・倍率が違う、を切り分け前に止める
            return f"撮った絵の大きさが {png_size(sheet)} で、期待した {(w, h * len(group))} と違う"
        split = f"[0]split={len(group)}" + "".join(f"[s{k}]" for k in range(len(group)))
        crops = "".join(f";[s{k}]crop={w}:{h}:0:{h * k}[o{k}]" for k in range(len(group)))
        outs = [x for k, st in enumerate(group) for x in ("-map", f"[o{k}]", str(shots[st]))]
        rc, msg = run_quiet(["ffmpeg", "-v", "error", "-y", "-i", str(sheet), "-filter_complex", split + crops, *outs])
        if rc != 0 or not all(png_size(shots[st]) == (w, h) for st in group):
            return f"切り分けに失敗 (rc={rc}): {msg[-300:]}"
        return None

    print(f"build: 見た目の状態 {len(states)} 種類を Chrome {len(groups)} 回で描く", file=sys.stderr)
    with concurrent.futures.ThreadPoolExecutor(max_workers=jobs) as ex:
        for gi, err in enumerate(ex.map(shoot, range(len(groups)))):
            if err:
                die(f"{gi + 1} 枚目のまとめ撮り: {err}")
    # 全部が同じ絵なら、まとめ撮りモードが効かずに同じ画面を撮っている (字幕も口も変わらない動画になる)
    if len(states) > 1 and len({hashlib.sha256(p.read_bytes()).digest() for p in shots.values()}) == 1:
        die("Chrome が撮った絵がすべて同じ。templates/player.html のまとめ撮りモード (#sheet=) が効いていない")

    runs = data["frames"]
    total = max(1, math.ceil(data["duration"] * MOUTH_FPS))
    listing = ["ffconcat version 1.0"]
    for i, r in enumerate(runs):
        end = runs[i + 1][0] if i + 1 < len(runs) else total
        listing += [f"file {ffq(shots[tuple(r[1:])])}", f"duration {(end - r[0]) / MOUTH_FPS:.6f}"]
    listing.append(f"file {ffq(shots[tuple(runs[-1][1:])])}")  # concat demuxer は最後の duration を使わないので 1 枚足す
    lst = td / "frames.ffconcat"
    lst.write_text("\n".join(listing) + "\n", encoding="utf-8")
    cmd = ["ffmpeg", "-v", "error", "-y", "-f", "concat", "-safe", "0", "-i", str(lst), "-i", str(m4a),
           "-map", "0:v", "-map", "1:a", *enc, "-pix_fmt", "yuv420p", "-r", str(MOUTH_FPS), "-fps_mode", "cfr",
           "-c:a", "copy", "-shortest", "-movflags", "+faststart", str(out)]
    r = subprocess.run(cmd, capture_output=True, text=True, stdin=subprocess.DEVNULL)
    if r.returncode != 0 or not out.is_file() or out.stat().st_size == 0:
        die(f"mp4 の書き出しに失敗 (rc={r.returncode}): {r.stderr.strip()[-400:]}")


def cmd_build(args: argparse.Namespace) -> None:
    script_path = Path(args.script).resolve()
    script = load_script(script_path)
    base = str(Path(args.output).resolve())
    for ext in (".html", ".mp4"):  # 付いていれば外す。v1.2 の ".2" のような拡張子でない部分は残す
        if base.lower().endswith(ext):
            base = base[: -len(ext)]
    formats = ["html", "mp4"] if args.format == "both" else [args.format]
    with tempfile.TemporaryDirectory() as tdname:
        td = Path(tdname)
        data, m4a = assemble(script_path, script, td, args.bitrate)
        for fmt in formats:
            out = Path(f"{base}.{fmt}")
            if fmt == "html":
                # mimetypes は .m4a を audio/mp4a-latm 等と推定することがあり、ブラウザが再生できない。容器の型を明示する
                out.write_text(render_html({**data, "audio": data_uri(m4a, "audio/mp4")}), encoding="utf-8")
            else:
                write_mp4(data, m4a, out, td, args.jobs)
            print(f"build: {fmt} / {len(data['lines'])} 行 / {data['duration']:.1f} 秒 / "
                  f"{out.stat().st_size / 1e6:.1f} MB → {out}", file=sys.stderr)


def jobs_arg(v: str) -> int:
    if not v.isdigit() or not 1 <= int(v) <= 16:
        raise argparse.ArgumentTypeError("1〜16 で指定する")
    return int(v)


def kbps_arg(v: str) -> int:
    m = re.fullmatch(r"(\d+)k?", v.strip().lower())
    if not m or not 8 <= int(m.group(1)) <= 320:
        raise argparse.ArgumentTypeError("8〜320 の kbps で指定する (例: 64k / 96)")
    return int(m.group(1))


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--engine", default=DEFAULT_ENGINE, help=f"VOICEVOX エンジンの URL (既定 {DEFAULT_ENGINE}、環境変数 VOICEVOX_URL)")
    sub = ap.add_subparsers(dest="cmd", required=True)
    sub.add_parser("check", help="必要なコマンド・コンテナ・エンジンの状態を確かめる (足りなければ rc=1)").set_defaults(func=cmd_check)
    sub.add_parser("up", help=f"エンジンをコンテナ {CONTAINER_NAME} で起動し、応答するまで待つ").set_defaults(func=cmd_up)
    sub.add_parser("down", help=f"up で起動したコンテナ {CONTAINER_NAME} を止める").set_defaults(func=cmd_down)
    sub.add_parser("speakers", help="話者とスタイル ID を一覧する").set_defaults(func=cmd_speakers)
    k = sub.add_parser("kana", help="文ごとの読み (audio_query の kana) を出す。read の候補を合成せずに比べる")
    k.add_argument("texts", nargs="*", help="読みを見たい文 (複数可)")
    k.add_argument("--script", help="台本の全行の読みを一覧する (synth 済みの行はキャッシュを使う)")
    k.add_argument("--who", choices=tuple(CAST), default="metan", help="声のキャラ (既定 metan)")
    k.add_argument("--style-id", type=int, help="声のスタイル ID (--who の既定の声より優先)")
    k.set_defaults(func=cmd_kana)
    s = sub.add_parser("synth", help="セリフごとに wav を合成する (<台本名>.work/ にキャッシュ)")
    s.add_argument("script")
    s.add_argument("--force", action="store_true", help="キャッシュを無視して作り直す (エンジンや辞書を更新したとき)")
    s.set_defaults(func=cmd_synth)
    b = sub.add_parser("build", help="合成済みの wav を連結し、HTML プレイヤーか mp4 (か両方) を書き出す")
    b.add_argument("script")
    b.add_argument("-o", "--output", required=True, help="出力先。拡張子は --format に合わせて付け替える (out → out.html / out.mp4)")
    b.add_argument("--format", choices=("html", "mp4", "both"), default="html", help="出力の形式 (既定 html)")
    b.add_argument("--jobs", type=jobs_arg, default=4, help="mp4 の絵を Chrome で並列に撮る数 (既定 4)")
    b.add_argument("--bitrate", type=kbps_arg, default=64, help="AAC のビットレート kbps (既定 64)")
    b.set_defaults(func=cmd_build)
    args = ap.parse_args()
    args.func(args)


if __name__ == "__main__":
    main()
