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

## 🚨 group issue の契約変更で射程が広がる（2026-09-06 追記）

issue [291](done/291-feat-glogx-epic-shows-done-children-by-default.md) で
**group issue の完了・保留先が global `issues/done/` から `issues/epic/<name>/done/`
(と `pending/`) へ変わった**（`1a1a21fa`）。移動の起点も行き先も 1 段深くなるので、
`../` の数がまた変わる:

| ファイルの位置 | `_claude/rules/x.md` を指す正しい形 |
|---|---|
| `issues/NNN.md` | `../_claude/rules/x.md` |
| `issues/done/NNN.md` | `../../_claude/rules/x.md` |
| `issues/epic/<name>/NNN.md` | `../../../_claude/rules/x.md` |
| `issues/epic/<name>/done/NNN.md` | `../../../../_claude/rules/x.md` |

**現時点の影響は 0 件**（実測 2026-09-06: `issues/epic/` は存在せず md も 0 件。
契約変更の前後で切れリンクは 189 件 / 77 ファイルのまま同じ）。つまりこれは
**これから起きる分**で、下の設計に効く:

- **一括修正を `issues/done/` 決め打ちの sed で書かない**。深さは
  「そのファイルの位置から repo root までの段数」で決まるので、**位置から導出**する
  （でないと group issue を直すときにもう一度同じ作業が要る）
- 🚨 **検査を「ディレクトリ名」で絞らない**。`done` / `pending` 決め打ちにすると、
  予約外の綴り（`closed/` `completed/` …）の配下に置かれた md を取りこぼす。291 の実装は
  そういう md を**迷子 `?` として一覧に出す**ように変えた（それまでは黙って消えていた）ので、
  「予約外の綴りにも md は在りうる」が前提になった
- 🚨 **再発防止の検査に `-maxdepth` を使わない**。`issues/epic/<name>/done/` は 4 段目なので、
  深さを決め打ちした検査は**新しい段を黙って対象外**にする
  （[`claude-md-maintenance.md`](../_claude/rules/claude-md-maintenance.md) の
  「ディレクトリ階層の契約を変えたら深さ前提の検査を grep する」がまさにこの形。
  実例として issue 番号一意の検査が旧深さのまま緑を出し続けたことが記録されている）。
  本 issue の「全数の数え方」に書いた `find issues -name '*.md'` は深さ非依存なのでそのまま使える

## 追測（2026-09-06、291 の実装後）

置き場所別に数え直した。**`done/` だけの問題ではない**:

| 置き場所 | 切れリンク |
|---|---|
| `issues/done/` | 182 件 |
| `issues/pending/` | **7 件** |
| `issues/` 直下 | 1 件 |

`pending/` にも同じ形（`../_claude/…` が 3 件、`done/NNN` が 1 件、兄弟 issue が 3 件）が出ている。
**修正も検査も `done/` 決め打ちにしない**根拠がここにある。

🚨 `issues/` 直下の 1 件は**この issue 自身**だった: 291 を参照していたが、その 291 が
`done/` へ移されて切れた（他セッションの正常な作業）。**この issue を書いている最中に、
この issue が言っている現象を踏んでいる**。頻度の証拠として残す。

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
