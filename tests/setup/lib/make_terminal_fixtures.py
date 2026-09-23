#!/usr/bin/env python3
"""mac/*.terminal から、テスト用の壊れた/細工した .terminal を生成する。

使い方: make_terminal_fixtures.py <正常な .terminal> <出力ディレクトリ>

tests/setup/test_terminal_profile_appearance.sh と test_terminal_profile_restore.sh の両方が
同じ fixture を使うのでここに寄せている (片方だけ直して検出力がずれるのを防ぐ)。

生成物:
  broken-keyed        色 blob が現行形式のまま破損
  broken-streamtyped  色 blob が旧 NSArchiver 形式のまま破損
  missing-font        実在しないフォント名 (利用可否 0 の経路)
  present-font        どの macOS にも同梱の Menlo-Regular (利用可否 1 の経路)
  no-font             Font キー自体が無い (旧いプロファイル書き出しとの互換)
  broken-font         Font blob が破損
  inject-font         フォント名に改行を仕込み、デコーダの出力へ行を注入しにいく
  huge-font           フォントサイズが Int64 超過 (Int() 変換で SIGTRAP になる形)
  system-font         `.AppleSystemUIFont` (= 修正前のバグが書き戻していた文字列)
  color-nan           色成分が NaN (Int() 変換で SIGTRAP になる形。inf/負値は NSColor がクランプする)

🚨 MISSING_FONT_NAME は「実在しない」ことが前提。実在する名前に変わると、利用可否 0 の
経路を一度も通らないまま緑になる。
🚨 逆に「利用可否 1」の経路を repo のプロファイル (HackNFP) で作らないこと。在庫判定はホストの
実フォントを見るので、フォント未導入の CI runner では在庫ゲートで落ちる (2026-09-23 に CI が赤)。
PRESENT_FONT_NAME は OS 同梱で必ず在る名前にしている。
"""
import plistlib
import sys

MISSING_FONT_NAME = "ZzQxNFP-Regular"
PRESENT_FONT_NAME = "Menlo-Regular"
INJECTED_COLOR = "BackgroundColor 65535 0 0"


def font_parts(blob):
    """Font blob (NSKeyedArchiver) から $objects を取り出す。"""
    arch = plistlib.loads(blob)
    objects = arch["$objects"]
    for i, o in enumerate(objects):
        if isinstance(o, dict) and "NSName" in o and "NSSize" in o:
            return arch, objects, i
    sys.exit("fixture: Font blob に NSFont の dict が無い")


def rebuild_color(blob, rgb):
    """色 blob (NSKeyedArchiver) の NSRGB 成分を差し替える。"""
    arch = plistlib.loads(blob)
    for o in arch["$objects"]:
        if isinstance(o, dict) and "NSRGB" in o:
            o["NSRGB"] = rgb.encode() + b"\x00"
            return plistlib.dumps(arch, fmt=plistlib.FMT_BINARY)
    sys.exit("fixture: 色 blob に NSRGB が無い")


def rebuild_font(blob, name=None, size=None):
    arch, objects, idx = font_parts(blob)
    if name is not None:
        objects[objects[idx]["NSName"].data] = name
    if size is not None:
        objects[idx]["NSSize"] = size
    return plistlib.dumps(arch, fmt=plistlib.FMT_BINARY)


def main():
    src, out = sys.argv[1], sys.argv[2].rstrip("/")
    d = plistlib.load(open(src, "rb"))

    def dump(name, mutate):
        copy = dict(d)
        mutate(copy)
        plistlib.dump(copy, open("%s/%s.terminal" % (out, name), "wb"))

    dump("broken-keyed", lambda c: c.__setitem__("BackgroundColor", b"bplist00" + b"\x00" * 20))
    # 旧 NSArchiver 形式のヘッダを持つ切り詰め blob (NSUnarchiver が例外を投げる形)
    dump("broken-streamtyped",
         lambda c: c.__setitem__("BackgroundColor", b"\x04\x0bstreamtyped\x81\xe8\x03\x84\x01" + b"\x00" * 8))

    font = d.get("Font")
    if font is None:
        sys.exit("fixture: プロファイルに Font キーが無い (テストの前提が崩れている)")
    dump("no-font", lambda c: c.pop("Font"))
    dump("broken-font", lambda c: c.__setitem__("Font", b"bplist00" + b"\x00" * 20))
    dump("missing-font", lambda c: c.__setitem__("Font", rebuild_font(font, name=MISSING_FONT_NAME)))
    dump("present-font", lambda c: c.__setitem__("Font", rebuild_font(font, name=PRESENT_FONT_NAME)))
    # 出力プロトコル (空白区切り 1 行 1 プロパティ) への行注入。後勝ちで色を上書きしにいく形。
    dump("inject-font",
         lambda c: c.__setitem__("Font", rebuild_font(font, name="%s 13 0\n%s\n" % (MISSING_FONT_NAME, INJECTED_COLOR))))
    dump("huge-font", lambda c: c.__setitem__("Font", rebuild_font(font, size=1e20)))
    dump("system-font", lambda c: c.__setitem__("Font", rebuild_font(font, name=".AppleSystemUIFont")))
    dump("color-nan", lambda c: c.__setitem__("BackgroundColor", rebuild_color(d["BackgroundColor"], "nan nan nan")))

    print(MISSING_FONT_NAME)


if __name__ == "__main__":
    main()
