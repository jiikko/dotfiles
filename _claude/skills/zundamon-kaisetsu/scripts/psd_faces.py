#!/usr/bin/env python3
"""PSDTool 形式の立ち絵 PSD から、表情 × 口の段階 (閉じ/半開き/開き) の画像を書き出す。

  uv run --with psd-tools python psd_faces.py <立ち絵.psd> <表情定義.json> <出力dir> [--preview sheet.png]

出力は <出力dir>/<表情>_0.webp (閉じ) / _1 (半開き) / _2 (開き) と、使った定義の写し faces.json。
dialogue_video.py は台本の cast.<キャラ>.faces にこの出力 dir を書くと読む。

表情定義 (faces/*.json):
  {"crop": [左, 上, 右, 下],          # PSD 座標で切り抜く範囲 (上半身など)
   "height": 900,                    # 書き出す高さ px (幅は比で決まる)
   "credit": "立ち絵: 作者名",          # 動画のクレジットに出す表記 (素材の規約に従う)
   "base": ["!眉/*普通眉", ...],      # 全表情に共通で当てる選択
   "faces": {"通常": {"layers": ["!目/*目セット", ...], "mouth": ["!口/*むふ", "!口/*ほあ", "!口/*ほあー"]}}}

レイヤーの指定は PSD のグループ名を / でつないだパス。PSDTool の作法どおり、名前が * で始まる
レイヤーは同じ親の中の * の付いた兄弟と排他 (1 つだけ表示) で、それ以外は指定すると表示になる。
指定したパスが PSD に無ければ止まる (素材の版でレイヤー名が変わったとき、黙って既定の絵を出さないため)。
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

try:
    from psd_tools import PSDImage
    from PIL import Image, ImageDraw, ImageFont
except ImportError:
    sys.exit("error: psd-tools が要る。uv run --with psd-tools python psd_faces.py ... で実行する")


def die(msg: str) -> None:
    print(f"error: {msg}", file=sys.stderr)
    sys.exit(1)


def select(psd, path: str) -> None:
    """パスの各段を表示にし、* の付いた段は同じ親の * の兄弟を非表示にする。"""
    node, parts = psd, path.split("/")
    for name in parts:
        layer = next((l for l in node if l.name == name), None)
        if layer is None:
            die(f"レイヤー {path!r} が PSD に無い ({name!r} で見つからない)")
        if name.startswith("*"):
            for sib in node:
                if sib.name.startswith("*"):
                    sib.visible = False
        layer.visible = True
        node = layer


def main() -> None:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("psd")
    ap.add_argument("spec")
    ap.add_argument("out")
    ap.add_argument("--preview", help="全表情 × 口 3 段階を並べた確認用の画像を書く")
    args = ap.parse_args()

    spec = json.loads(Path(args.spec).read_text(encoding="utf-8"))
    faces = spec.get("faces") or {}
    if not faces:
        die(f"{args.spec}: faces が空")
    for name, f in faces.items():
        if len(f.get("mouth", [])) != 3:
            die(f"{args.spec}: faces.{name}.mouth は 閉じ / 半開き / 開き の 3 つ")
    crop = tuple(spec["crop"])
    height = int(spec.get("height", 900))

    psd = PSDImage.open(args.psd)
    initial = {id(l): l.visible for l in psd.descendants()}
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    tiles = []
    for name, f in faces.items():
        for lv, mouth in enumerate(f["mouth"]):
            for l in psd.descendants():  # 表情ごとに PSD の既定の表示状態から始める (前の表情の選択を持ち越さない)
                l.visible = initial[id(l)]
            for path in [*spec.get("base", []), *f.get("layers", []), mouth]:
                select(psd, path)
            im = psd.composite(force=True).crop(crop)
            im = im.resize((round(im.width * height / im.height), height), Image.LANCZOS)
            im.save(out / f"{name}_{lv}.webp", "WEBP", quality=90)
            tiles.append((f"{name} {lv}", im))
        print(f"{name}: 3 枚", file=sys.stderr)
    (out / "faces.json").write_text(json.dumps({"faces": list(faces), "credit": spec.get("credit")}, ensure_ascii=False), encoding="utf-8")

    if args.preview:
        tw, th = tiles[0][1].width // 3, tiles[0][1].height // 3
        sheet = Image.new("RGB", (tw * 3, (th + 24) * len(faces)), "white")
        draw = ImageDraw.Draw(sheet)
        try:  # 表情名は日本語なので、あれば日本語フォントで書く
            font = ImageFont.truetype("/System/Library/Fonts/ヒラギノ角ゴシック W6.ttc", 18)
        except OSError:
            font = ImageFont.load_default()
        for i, (label, im) in enumerate(tiles):
            x, y = (i % 3) * tw, (i // 3) * (th + 24)
            small = im.resize((tw, th))
            sheet.paste(small, (x, y + 24), small)
            draw.text((x + 4, y + 2), label, fill="black", font=font)
        sheet.save(args.preview)
    print(f"psd_faces: {len(faces)} 表情 x 3 → {out}", file=sys.stderr)


if __name__ == "__main__":
    main()
