# 414 (docs): global rule の常時読み込みを減らし、上限 150k 字までの余裕を取り戻す

起票日: 2026-09-24

## 概要

Claude Code は起動時に、常時読み込む指示ファイルの合計が 150k 字を超えると次の警告を出す。

```
⚠ 62 instruction files add up to 193.4k chars, over the 150.0k-char total limit
```

2026-09-24 に my-products 側（umbrella `f4cacb9`、obaket `fae16d19`）で repo の rule に `paths:` を付け、
obaket のセッションでは上限を下回る見込みになった。ただし残りの大半は global 層
（`~/.claude/CLAUDE.md` と `_claude/rules/`）が占めていて、余裕がほとんど無い。
**global の rule が 2〜3 本増えると、どの repo でも再び上限を超える。**

## 現状（実測 2026-09-24、`wc -m`。`paths:` 付きの rule は除く）

| 層 | 常時読み込み |
|---|---|
| global（`~/.claude/CLAUDE.md` 10.9k + `_claude/rules/` の 35 本） | 118.5k |
| my-products umbrella（`CLAUDE.md` + `.claude/rules/`） | 12.4k |
| obaket（`CLAUDE.md` + `.claude/rules/`） | 12.3k |
| dotfiles repo（`CLAUDE.md` + `.claude/rules/`） | 7.9k |

- obaket のセッション: 118.5 + 12.4 + 12.3 = **143.2k**（残り 約 7k）
- dotfiles のセッション: 118.5 + 7.9 = 126.4k
- 警告の値に含まれるのは `paths:` の無いファイルだけ（193.4k は、`paths:` 付きの rule を除いた合計とほぼ一致した。
  thumbnailthumb 側のセッションが別途確かめた値は 215.7k）
- user 層（`~/.claude/rules/` = `_claude/rules/` への link）でも `paths:` が効くことは、既に `paths:` 付きの
  `no-comment-line-starting-with-shellcheck.md` / `shell-numeric-gate-explicit-digits.md` が警告の集計から外れていることで確認できる
- 上限を超えたときに内容が切り捨てられるのか、警告が出るだけなのかは**未確認**

## 対応方針の候補（見込みの大きい順。字数は `wc -m`）

### A. `mutation-verify-new-tests.md`（10.8k）に `paths:` を付ける

発動点が「新規テストを書き終えた / commit する」だけにある、最大の rule。テストファイルを Read したときだけ読み込めば足りる。

- glob は repo ごとにテストの置き場所が違う（Go は `src/<proj>/*_test.go`、shell は `tests/`、Swift は `*Tests/`）ので、
  全部を覆う形を実測してから決める
- 🚨 **`_claude/CLAUDE.md` の「設計方針」表がこの rule を「新規テストを commit する」の発動点として指している**。
  テストファイルを読まずに commit まで進む経路が残るので、索引の文言を「読み込まれないときは直接 Read する」に揃える

**テスト系に見えるが、対象から外すもの**（反証レビューで確認。テスト以外にも発動点がある）:

| rule | 字数 | テスト以外の発動点 |
|---|---|---|
| `avoid-wall-clock-assertions.md` | 4.3k | ②「何かが起きるのを待つために `sleep` を書こうとした瞬間」（一般のシェルスクリプトも対象。`_claude/CLAUDE.md` の索引も 2 つの発動点を分けて書いている） |
| `verify-interactive-prompt-with-pty-driver.md` | 2.0k | 「対話プロンプトを持つコマンドを自動で動かして確認しようとした瞬間」（テストに限らない） |
| `sandbox-real-destructive-test-apis.md` | 2.6k | 「検査と破壊的操作は隣接させる」節は production の削除エンジン（148 段階 ④）が起源。なお、この rule は `_claude/CLAUDE.md` の索引表に載っていない |

### B. Apple / Xcode の作業でしか発動しない rule に `paths:` を付ける（約 4.9k）

| rule | 字数 |
|---|---|
| `no-osascript-for-ui-verification.md` | 2.3k |
| `no-concurrent-spm-build-during-xcodebuild.md` | 2.0k |
| `no-ios-simulator-verification.md` | 0.6k |

glob の候補は `**/*.swift` / `**/project.yml` / `**/Package.swift` / `**/*.xcodeproj/**`。

### C. dotfiles でしか発動しない rule を dotfiles repo の `.claude/rules/` へ移す（見込みは小さい）

2026-09-24 の最初の提案はこれだったが、実測では候補が少ない。

- `tmux-probe-requires-socket-isolation.md`（4.5k）が最大だが、**Claude はどの repo でも tmux のペインの中で動く**ので、
  dotfiles 専用とは言えない（発動点は「破壊的な tmux コマンドを打つ前」で、repo を問わない）。
  致命的な部分は `bin/tmux` の shim と PreToolUse hook が強制済みなので、移すなら「md に残る規律
  （隔離の実証 / 依頼外の kill を足さない / `2>/dev/null` 禁止）が他 repo で要らないか」を先に判断する
- `path-shim-must-resolve-real-binary.md`（1.8k）と `no-mixed-width-columns-in-terminal-ui.md`（1.4k）は、
  起源が dotfiles というだけで、発動点は repo に依存しない

### D. issue 運用でしか発動しない rule に `paths:` を付ける（約 9.5k。要検討）

| rule | 字数 | 発動点 |
|---|---|---|
| `claim-issue-in-next-and-push.md` | 3.8k | `issues/next/` がある repo で着手するとき（本文で自ら「無ければ読み飛ばしてよい」と宣言している） |
| `move-report-conclusions-to-issues.md` | 3.6k | `./tmp/*.md` にレポートを書き出したとき |
| `issue-creation-codex-review.md` | 2.1k | issue を新規作成・大幅改訂したとき |

- 🚨 発動点が「ファイルを Read したとき」ではなく「**着手・作成・書き出しをしたとき**」なので、`paths:` が Read だけを起点にするなら
  取りこぼす（claim は `ln -s` だけで済み、issue を Read しないことがある）。Write / Edit でも読み込まれるのかを先に実測する
- issue 運用の規約は SessionStart hook（`_claude/hooks/issue-rules-inject.sh`）でも注入されているので、そちらへ寄せる手もある

### やらないこと

- 本文を要約して縮めること（74d23ce7 で経緯は rules-rationale へ移し済み。これ以上削ると規律そのものが落ちる）

## 受け入れ条件

- [x] A / B / C / D のどれを採るかを決め、採らないものは理由を 1 行書く（下の「決定」）
- [x] 採った rule に `paths:` と理由コメントを付ける（書式は `shell-numeric-gate-explicit-digits.md` と同じ）
- [x] `~/.claude/CLAUDE.md` の索引表で、`paths:` 付きにした rule の発動点を「読み込まれないときは直接 Read する」に揃える
- [x] 採った glob で実際に読み込まれることを確かめる（`claude -p --model haiku --settings '{"disableAllHooks":true}' --allowedTools=Read`
      でテストファイルを Read させる / 何も Read させない、の 2 通り。hook を切らないと Stop hook が応答を上書きする）
- [ ] obaket のセッションの常時読み込み合計を測り直し、結果をこの issue に書く

## 関連ファイル

- `_claude/rules/`（global rule の正本）/ `_claude/CLAUDE.md`（索引表）
- my-products の `CLAUDE.md` と `apps/obaket/CLAUDE.md` の「`paths:` 付き rule」索引表（同じ形の先行例）

## 進捗

- 2026-09-24 起票（obaket のセッションで実測）。起票前の反証レビュー（sonnet 1 体）で、案 A に入れていた 3 本（avoid-wall-clock / pty-driver / sandbox-destructive）がテスト以外でも発動すると指摘され、A から外した。issue 系の 3 本の見落としも指摘され、D として足した。字数・パス・commit・案 B・案 C の判断は反証されなかった
- 2026-09-25（dotfiles-7d）: A と B の一部を実施（commit「docs(claude): mutation-verify と Apple 専用の 2 本を paths: で条件ロードにする (issue 414)」）

### 決定

- **A 却下（一度入れて戻した）**: eb0bc7a7 で `paths:` を付けたが、dotfiles の `CLAUDE.md`「`_claude/` を触るとき」が
  「行動で発火するルール（commit / tmux / **テスト作法**）は無条件のまま置く」と名指ししており、mutation-verify はそのテスト作法
  そのもの。発動点は「新規テストを commit する / green を確認した瞬間」で、テストを Write で書いて commit する流れでは
  Read が挟まらず読み込まれない（索引の「直接 Read する」は保証にならない）。dotfiles-5c の指摘で無条件に戻した
- **B は 2 本だけ採用**: `no-osascript-for-ui-verification.md` / `no-concurrent-spm-build-during-xcodebuild.md` に
  Swift / Xcode 系の `paths:`。**`no-ios-simulator-verification.md` は常時読み込みに残す**: 発動点が「`make test` を打つ」
  という行動で、Swift を 1 つも Read せずに打つ経路がある（「make test して」と頼まれた直後など）。0.6k と小さく、得るものも少ない
- **C 却下**: 候補の 3 本はどれも発動点が repo に依存しない（上の C 節の分析のとおり）。移すと他 repo で規律が消える
- **D 見送り**: 発動点が行動（着手・作成・書き出し）で、`paths:` は Read でしか発火しない（Write / Edit では発火しないことは
  dotfiles の `CLAUDE.md` に実測つきで注記済み）。`_claude/issue-rules.md` の hook 注入へ寄せる手は、拘束力（system-reminder）と
  link の張り方が変わるので今回はやらない。**再開の trigger**: B の後でも常時読み込みが再び 140k を超えたとき

### 結果（`wc -m`、`paths:` の無いファイルだけ）

| | 前 | 後 |
|---|---|---|
| global の rule（`_claude/rules/`） | 109.1k | 104.8k |
| `~/.claude/CLAUDE.md`（Apple の 2 本の索引に注記を足した分だけ増えた） | 12.0k | 12.3k |
| global 計 | 121.1k | **117.1k** |
| dotfiles のセッション（repo 層 7.9k を足す） | 129.0k | **125.0k**（実測） |
| obaket のセッション（repo 層は起票時の 12.4k + 12.3k） | 145.8k | **141.8k**（見積もり。下記） |

A を戻した後の値（`wc -m`、2026-09-25）。A を入れていた間は global 計 105.8k だった

起票時の global 118.5k は、その後に rule が増えて 121.1k になっていた。

### 読み込まれることの確認（`claude -p --model haiku --settings '{"disableAllHooks":true}' --allowedTools=Read`）

| cwd | Read させたもの | 問い（rule の本文にしか無い文） | 答え |
|---|---|---|---|
| dotfiles | なし | mutation-verify の見出し「変異は「production の機構を戻す」形にする」 | NO（A を入れていた間の測定。A は戻した） |
| dotfiles | `tests/tmux/test_tmux_toast.sh` | 同上 | YES（同上） |
| DualNoteApp | なし | no-concurrent-spm の「Reload Package が最終行のまま…」 | NO |
| DualNoteApp | `Shared/Package.swift` | 同上 | YES |

較正: 常時読み込みの rule の文は Read なしで YES、存在しない文は NO。🚨 最初は問いの文が索引に足した説明と重なっていて、
Read なしでも YES になった（索引を見て答えていた）。rule の本文にしか無い文へ替えて測り直した

### 残タスク

- [ ] obaket のセッションの常時読み込み合計の実測（このマシンに my-products の checkout が無く測れなかった。上の 141.8k は
      起票時の repo 層の値を足した見積もり）。受け入れ条件の最後の項目。obaket のあるマシンのセッションで `wc -m` を取り直す
- 2026-09-25（dotfiles-7d）: A を戻した（commit「docs(claude): mutation-verify の paths: を外し、無条件の読み込みに戻す (issue 414)」）。
  B だけでは obaket の見積もりが 141.8k で、上限までの余裕は約 8k しか戻っていない。D（issue 運用の 3 本 9.5k を hook 注入へ寄せる）を
  検討する時期は近い
