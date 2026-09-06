# issue を `done/` へ移すと本文の相対リンクが切れる（実測 189 件 / 77 ファイル）

- 種別: bug（ドキュメントの参照切れ）
- 対象: `issues/done/**.md`、検査の不在（`tests/issues/` には番号一意と next symlink の検査しかない）
- 優先度: medium

## 症状

issue の本文リンクは `issues/` 直下を起点に書かれているので、`git mv issues/NNN-x.md issues/done/`
した瞬間に**全部 1 段ずれる**。CLAUDE.md は claim symlink の文脈で
「rename すると本文の相対リンクが切れる」と警告しているが（issue 263）、**`done/` への移動には
同じ警告が効いていない**（そちらは日常操作なので、むしろ頻度が高い）。

## 全数（2026-09-06 実測）

```
切れリンク合計: 189 件 / 該当ファイル 77 本
参照先の先頭要素:  _claude 97 / done 57 / next 4 / src 2 / その他 29
issues/done/ 以外にあるもの: 7 件（別原因）
```

数え方（`issues/` 配下の `*.md` から `](…​.md)` を抜き、ファイルの位置から解決する）:

```sh
find issues -name '*.md' -print0 | while IFS= read -r -d '' f; do
  d=$(dirname "$f")
  grep -oE '\]\((\.\./)*[A-Za-z0-9_./-]+\.md\)' "$f" | sed 's/^](//;s/)$//' | while read -r l; do
    [ -e "$d/$l" ] || echo "$f|$l"
  done
done
```

## 原因は 2 パターンだけ（機械的に直せる）

| パターン | 件数 | 例 | 正しい形 |
|---|---|---|---|
| `done/` の中から `done/NNN-x.md` を指す | 57 | `issues/done/194-*.md` → `done/172-*.md` | `172-*.md` |
| `done/` の中から `../_claude/…` を指す | 97 | `issues/done/133-*.md` → `../_claude/rules/…` | `../../_claude/rules/…` |

残り 35 件は個別（移動先が `done/` でない / 参照先自体が消えている等）なので、まとめて直さず 1 件ずつ見る。

## 提案

1. 上の 2 パターンを機械置換で直す（154 件）
2. 残り 35 件を個別に判断（参照先が消えているものは参照ごと消すか、移動先へ張り直す）
3. **再発を止める検査を `tests/issues/` に足す**。既存の `test_next_links_valid.sh` と同じ場所・同じ形
   - 🚨 検査を新設するので [`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)
     の §0-B（既に答えを出している経路は無いか）と §8（脅威モデルと「検出しない形」を先に書く）を通すこと
   - 「検出しない」を先に決める候補: 外部 URL / アンカーのみ（`#…`）/ コードブロック内の例示パス
   - 🚨 抽出が空だと「違反 0 件 = 緑」になるので、**本走査と同じ関数を通る canary** と
     走査件数の下限を置く（[`verify-execution-not-just-exit-code.md`](../_claude/rules/verify-execution-not-just-exit-code.md)）

## なぜ今直さないか

このセッション（retro [290](done/290-retro-glogx-audit-x2-and-21-issues-2026-09-06.md) の切り出し）で
**自分が 290 を `done/` へ移した際に同じ壊れ方をして気づいた**もので、その 1 件は直した。
残りは 77 ファイル・依頼と無関係な範囲なので、まとめて直すかは別途判断する。
