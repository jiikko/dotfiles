"""ベンチマークの入力を、repo に commit 済みの文章から決まった手順で組み立てる。

    python3 -I gen.py <出力先の dir>

入力の元は testdata/corpus/ (dotfiles の文書と本家 yomiyasu の tests/corpus。MIT) だけで、乱数も時刻も使わない。
同じ commit なら同じバイト列になり、manifest.tsv に各ファイルの SHA-256 を書く。
組み立てた文章を commit しないのは、元の文章と同じ中身を何倍にも複製して repo を太らせないため。
"""

import hashlib
import os
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
CORPUS = HERE.parent / "testdata" / "corpus"
UP = CORPUS / "upstream"


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def md_files(d: Path) -> list:
    files = sorted(p for p in d.iterdir() if p.suffix == ".md")
    if not files:
        raise SystemExit(f"gen.py: {d} に .md が無い")
    return files


def fill(parts: list, limit: int) -> str:
    """parts を順に繰り返して並べ、limit バイトを超えない最後の段落の切れ目で切る。"""
    return fill_count(parts, limit)[0]


def fill_count(parts: list, limit: int) -> tuple:
    """fill と同じに並べ、並べた parts の数も返す。"""
    out, size, i = [], 0, 0
    while True:
        part = parts[i % len(parts)].rstrip("\n") + "\n\n"
        n = len(part.encode("utf-8"))
        if size + n > limit:
            break
        out.append(part)
        size += n
        i += 1
    if not out:
        raise SystemExit(f"gen.py: 最初の段落が {limit} バイトに収まらない")
    return "".join(out), len(out)


def paragraphs(text: str) -> list:
    return [p for p in text.split("\n\n") if p.strip()]


def cut_chars(s: str, limit: int) -> str:
    """UTF-8 で limit バイトを超えない所まで、字の境目で切る。"""
    b = s.encode("utf-8")[:limit]
    return b.decode("utf-8", errors="ignore")


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("使い方: python3 -I gen.py <出力先の dir>")
    out = Path(sys.argv[1])
    out.mkdir(parents=True, exist_ok=True)

    docs = [read(p) for p in md_files(CORPUS / "dotfiles") + md_files(CORPUS / "references")]
    doc_paras = [p for d in docs for p in paragraphs(d)]
    raw_names = [p.name for p in md_files(UP / "raw_ai") if (UP / "yomiyasu_rewritten" / p.name).exists()]
    raw = [read(UP / "raw_ai" / n) for n in raw_names]
    rewritten = [read(UP / "yomiyasu_rewritten" / n) for n in raw_names]
    ai = raw + [read(p) for p in md_files(UP / "blacklist_ai")]
    ai_sentences = [s.strip() + "。" for t in raw for s in t.replace("\n", "").split("。") if len(s.strip()) >= 12]

    files = {}
    # 長さ: dotfiles と本家の references の段落を順に並べる (4 KB から 256 倍。元は約 570 KB なので 1 MB は 2 周目に入る)
    for kb in (4, 16, 64, 256, 1024):
        files[f"len-{kb:03d}k.md"] = fill(doc_paras, kb * 1024)
    # 同じ段落の繰り返し
    first = next(p for p in doc_paras if len(p.encode("utf-8")) >= 300)
    files["rep-064k.md"] = fill([first], 64 * 1024)
    # 表と箇条書き (太字のラベル・入れ子) が多い
    # 20 行ずつの表と 10 項目ずつの箇条書きを交互に置く (1 つの塊が大きいと 64 KB の手前で切れて小さくなる)
    blocks = []
    for start in range(0, len(ai_sentences), 20):
        chunk = ai_sentences[start : start + 20]
        rows = "\n".join(f"| {start + j + 1} | {s} | 確認済み |" for j, s in enumerate(chunk))
        blocks.append("| 番号 | 文 | 状態 |\n|---|---|---|\n" + rows)
        blocks.append(
            "\n".join(f"- **項目{start + j + 1}**: {s}\n  - {chunk[(j + 1) % len(chunk)]}" for j, s in enumerate(chunk[:10]))
        )
    files["table-064k.md"] = fill(blocks, 64 * 1024)
    # AI が書きがちな語が多い (本家の raw_ai と blacklist_ai)
    files["ai-064k.md"] = fill(ai, 64 * 1024)
    # 改行の無い 1 行
    files["line-032k.md"] = cut_chars("".join(ai_sentences) * 4, 32 * 1024).rstrip() + "\n"
    # diff と check の組 (直す前と後)。小は 1 組、大は全部の組を同じ順に並べる
    files["pair-small-before.md"] = raw[0]
    files["pair-small-after.md"] = rewritten[0]
    for kb in (64, 256):
        files[f"pair-{kb:03d}k-before.md"], n = fill_count(raw, kb * 1024)
        files[f"pair-{kb:03d}k-after.md"] = "".join(rewritten[i % len(rewritten)].rstrip("\n") + "\n\n" for i in range(n))

    lines = []
    for name in sorted(files):
        data = files[name].encode("utf-8")
        (out / name).write_bytes(data)
        lines.append(f"{name}\t{len(data)}\t{hashlib.sha256(data).hexdigest()}")
    (out / "manifest.tsv").write_text("\n".join(lines) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
