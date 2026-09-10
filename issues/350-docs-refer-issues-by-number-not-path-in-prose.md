# 散文の issue 参照は `issues/NNN-….md` のパスでなく番号で書く（パスは done 移動で腐る）

起票日: 2026-09-11
対応日: 2026-09-11
出典: [issue 348](done/348-retro-issue-done-tool-and-truecolor-guard-2026-09-10.md) 気づき 8

## 問題

`issue_done.sh` が張り直すのは markdown リンク（`](…)`）だけ。インラインコードや散文に書かれた
`` `issues/226-….md` `` のような裸のパスは対象外で、`tests/issues/test_issue_links_valid.sh` も
`issues/` 配下しか見ない。**issue を done へ送るたびに誰にも気づかれず 1 本ずつ切れる**。
2026-09-10 の全数勘定で 10 件あり（`nvim/lua/dotfiles/lsp.lua` / `src/lockman/main.go` ×2 /
`_claude/rules-rationale/` 3 本 / `_claude/rules/` 4 本）、その場で直した。

**後日確認（2026-09-11）**: `issue_done.sh` の事後条件 `stale_ref_report` も同じ `AWK_REBASE` を
`report=1` で呼ぶ実装なので、`](…)` しか見ない。移動時のゲートも裸パスには効いていなかった。

## 方針: (c) 番号で参照する

`issues/README.md` の「ファイル名は変えないので `issue 012` で安定して参照できる」が本来の意図。
(b) `issue_done.sh` を裸パスにも広げる案は却下: `issues/030-feat-a.md` のようなテスト fixture が
同じ形をしており、書き換えてはいけない対象と区別できない。

## 受け入れ条件

- [x] `issues/README.md` に「issues/ の外（コード・rules・docs）からは番号で参照する。パスを書くなら markdown リンクにする」を 1 行足す
- [x] `issues/` の外にある裸の `issues/NNN-…md` パスを全数勘定し、番号参照へ書き換える（前回 10 件 → 現在の件数を本文に書く）
- [x] 再発を止める検査を入れるか判断する（入れないなら理由をここに書く）→ **入れた**（判定軸は「番号」ではなく「番号 + スラッグ」。下記）

## 進捗

| 項目 | 状態 | 対応 commit |
|---|---|---|
| README への規範追記 | 完了 | docs(350): issues/ の外からの issue 参照を番号へ揃え、腐りを検査する |
| 裸パスの書き換え | 完了（43 箇所 / 30 ファイル） | 同上 |
| 再発検査 `tests/issues/test_issue_path_refs_not_stale.sh` | 完了 | 同上 |

`issues/README.md` は新しい行を足さず、**11 行目（採番の説明）を延長**した。同じことを 2 箇所に
書くと片方が腐るため。

## 結果: 全数勘定（2026-09-11、origin/master 432b8ba9 起点）

抽出は「`](…)` のリンク先を潰してから `issues/(done/|pending/|…)?NNN-slug.md` を拾う」。
`issues/` 配下は既存の `test_issue_links_valid.sh` の担当なので除外。**53 行 / 160 一致**が母集合。

| 分類 | 件数 | 処置 |
|---|---|---|
| dotfiles 自身の issue を指す裸パス | 34 行 | `issue NNN` へ書き換え |
| 他 repo（obaket / ThumbnailThumb / VLCMultiVideoPlayer）の issue パス | 12 行 | `obaket issue 724` の形へ。**repo 名は必ず残す**（dotfiles の採番もいずれ 724 に達するので、裸の `issue 724` は曖昧になる） |
| 他 repo 向けのテンプレート・作成するファイル名・起動例 | 7 行 | **据え置き**（下記の却下理由） |

置換は 43 箇所（1 行に 2 番号ある行があるため行数と一致しない）。30 ファイル。

### 移動で実際に切れていた参照: 2 件

前回（10 件）の後にも腐りは発生していた。どちらも新設した検査が検出した。

- `src/lockman/README.md:6` — markdown リンクの**表示文字列**が `issues/091-….md` のまま
  （リンク先は `done/` を指していて正しいので、リンク検査では絶対に見つからない形）
- `tests/zshrc/test_print_p_injection.sh:9` — `# 規範: issues/089-….md`

### 据え置いた 7 行と却下理由

**書き換えると意味が壊れるので直さない**（次の監査が同じ指摘を再生成しないよう記録する）。
いずれも新設した検査では誤検出にならない（スラッグが実在の dotfiles issue と一致しないため）。

| 箇所 | 却下理由 |
|---|---|
| `_claude/agents/crash-analyzer.md:150,155` | issue ファイル名の**書式そのもの**とその例。参照ではない |
| `_claude/agents/crash-analyzer.md:283` | これから**作るファイル名**。番号に置き換えると手順が成立しない |
| `_claude/agents/tt-api-expert.md:108,132` | ThumbnailThumb 側の issue を書式の見本として指している。dotfiles の移動では腐らない |
| `_claude/skills/codex-lead/SKILL.md:28` | `/codex-lead <path>` に**パスを渡せる**ことの実演。番号にすると例の意味が変わる |
| `_claude/skills/avfoundation-reference/SKILL.md:156,157` | 直前の見出しが VLCMultiVideoPlayer repo で、その配下の**ファイル一覧**。他のファイルパスと並んでいる |

## 結果: 新設した検査

`tests/issues/test_issue_path_refs_not_stale.sh`（`make test` の自動発見で走る。
`make test-dir DIR=tests/issues` で出力行を確認済み）。

**判定軸は「番号 + スラッグの一致」**。書かれたパスが解決せず、かつ**同じ番号かつ同じスラッグ**の
issue が `issues/` 配下に実在するとき、その issue が動いた = 腐りと判定する。

番号だけで照合すると、テスト fixture（`issues/186-x.md`）と他 repo の issue が実在の dotfiles issue と
番号衝突して偽陽性になる。スラッグまで見ると**除外リストが 1 件も要らない**（全数照合で確認）。
`tests/` を走査から外していないのはこのため — 母集合の大半が fixture で、外すと候補が 1 桁に落ちて
「抽出が壊れても違反 0 件 = 緑」に戻る。

### 検出しない形（射程。§8 の宣言）

- **glob / 省略を含む参照**（`issues/done/189-*.md` / `issues/done/010-...`）。リテラルとして
  解決せず、スラッグも `*` なので原理的に見えない。**今回はこの形の 4 件（010 / 189 / 208 / 222）を
  番号参照へ畳んで母集合から消した**が、新しく書かれたものは検出できない
- **他 repo の issue パス**。この repo からは正しさを判定できない
- **`](…)` の markdown リンク**。`issue_done.sh` の手順 4 と `stale_ref_report` が持ち主

### 変異検証（5 本すべて red を確認、2026-09-11）

| 変異 | 結果 |
|---|---|
| M1 `src/lockman/README.md` の表示文字列を裸パスへ戻す | red（実バグの再現） |
| M2 `tests/zshrc/test_print_p_injection.sh` の規範コメントを戻す | red（実バグの再現） |
| M3 スラッグ判定を外し番号だけで照合する | red（canary 3 件 / 本走査 102 件の偽陽性） |
| M4 抽出の正規表現を壊す | red（候補 0 件 → 空振り検出） |
| M5 最初の 1 件しか判定しない | red（canary の腐りを**先頭以外**に置いてある） |

合格側（§4）: 本走査は fixture 113 件と他 repo 参照を含んだまま腐り 0 件で緑。
M3 を当てて初めて赤くなるので、緑は「何も見ていない緑」ではない。

canary は本走査と**同じ `report` / `scan_tree` / `build_index`** を通す（式のコピーではない）。
候補件数と腐り件数の両方を固定しているので、抽出だけ壊れても判定だけ壊れても落ちる。

## 残タスク

なし。スコープ外として明示するもの:

- glob / 省略形の参照は検出できない（上記のとおり射程の外。今ある 4 件は畳んだ）
- 他 repo 側の issue パスの腐りは、その repo で直す
