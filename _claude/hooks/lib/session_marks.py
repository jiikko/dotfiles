"""hook が session ごとに残す印 (「この session には案内済み」「最後に案内した時刻」) を、1 つのファイルに行で持つ。

行の形は「session<TAB>種類<TAB>値」。種類ごとに、同じ session の最後の行の値が今の値になる。
ファイルが MAX_BYTES を越えたら古い半分を捨てる (捨てた行の session は、もう 1 回案内されうる)。

なぜ: session ごとの空ファイルを $TMPDIR に作ると、macOS では数日たっても片付かず溜まる (2026-11-10 に 73 件・最古は 7 日前)。
読み書きは 1 か所に置く: 同じ仕組みを hook ごとに写すと、上限・lock・壊れた行の扱いが写しの間でずれる。

使い方:
  python から: sys.path に hooks/lib を足して import session_marks。load / last / put
  シェルから:  python3 -I session_marks.py last <ファイル名> <session> <種類>   → 値を出す (無ければ rc=1)
               python3 -I session_marks.py put  <ファイル名> <session> <種類> <値>
  ファイル名は ${XDG_CACHE_HOME:-~/.cache} の下の名前。

🚨 どの失敗も hook を止めない (印が読めなければ「案内していない」として扱う)。書けない・詰め直せないときは
cache の claude-hook-marks.log に「時刻 warn 理由」の行を足し、stderr にも出す (rc=0 の hook の stderr はモデルにも画面にも出ない)。
"""
import fcntl
import os
import re
import sys
import time

MAX_BYTES = 64 * 1024


def cache_dir():
    return os.environ.get("XDG_CACHE_HOME") or os.path.expanduser("~/.cache")


def path_of(name):
    return os.path.join(cache_dir(), name)


def safe_session(session):
    return re.sub(r"[^A-Za-z0-9_-]", "_", str(session or "unknown"))


def warn(msg):
    print("session-marks: " + msg, file=sys.stderr)
    try:
        with open(os.path.join(cache_dir(), "claude-hook-marks.log"), "a") as f:
            f.write("%s\twarn\t%s\n" % (time.strftime("%Y-%m-%dT%H:%M:%S%z"), re.sub(r"[\t\n]", " ", msg)))
    except OSError:
        pass


def load(path, session, on_warn=None):
    """session の (種類, 値) の列をファイルの順に返す。読めなければ空 (案内をもう 1 回出すだけで、止めない)。"""
    prefix = session + "\t"
    out = []
    try:
        with open(path, encoding="utf-8", errors="replace") as f:
            for line in f:
                if not line.startswith(prefix):
                    continue
                parts = line.rstrip("\n").split("\t", 2)
                if len(parts) == 3:
                    out.append((parts[1], parts[2]))
    except FileNotFoundError:
        pass
    except OSError as e:
        (on_warn or warn)("印のファイルを読めない (%s): %s" % (path, e))
    return out


def last(path, session, kind):
    """session の種類 kind の最後の値。無ければ None。"""
    value = None
    for k, v in load(path, session):
        if k == kind:
            value = v
    return value


def put(path, session, kind, value, on_warn=None):
    """印の行を足す。MAX_BYTES を越えたら古い半分を捨てる。並行する session と同じファイルを書くので、追記と詰め直しを同じ lock の中で行う
    (lock の外で詰め直すと、その間の他の追記を置き換えで失う)。行の頭にも改行を付ける: 途中で切れた行 (書き込みの失敗・手で書いた行) に
    次の印が連結されて読めなくなるのを防ぐ (空行は load が読み飛ばし、詰め直しで消える)。詰め直せないと上限なく伸びるので、失敗は warn に出す。"""
    value = re.sub(r"[\t\n]", " ", str(value))
    try:
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path + ".lock", "a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            with open(path, "a", encoding="utf-8") as f:
                f.write("\n%s\t%s\t%s\n" % (session, kind, value))
            if os.path.getsize(path) > MAX_BYTES:
                with open(path, encoding="utf-8", errors="replace") as f:
                    lines = [l for l in f if l.strip()]
                with open(path + ".tmp", "w", encoding="utf-8") as f:
                    f.writelines(lines[len(lines) // 2:])
                os.replace(path + ".tmp", path)
    except OSError as e:
        (on_warn or warn)("印のファイルに書けない・詰め直せない (%s): %s" % (path, e))


def main(argv):
    try:
        if len(argv) == 5 and argv[1] == "last":
            v = last(path_of(argv[2]), safe_session(argv[3]), argv[4])
            if v is None:
                return 1
            print(v)
            return 0
        if len(argv) == 6 and argv[1] == "put":
            put(path_of(argv[2]), safe_session(argv[3]), argv[4], argv[5])
            return 0
        warn("使い方の誤り: %r" % (argv[1:],))
        return 2
    except Exception as e:  # 想定外の失敗も hook を止めない
        warn("想定外の失敗: %r" % (e,))
        return 2


if __name__ == "__main__":
    sys.exit(main(sys.argv))
