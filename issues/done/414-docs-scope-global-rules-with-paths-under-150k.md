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
- [x] obaket のセッションの常時読み込み合計を測り直し、結果をこの issue に書く (143.3k。下の「obaket の実測」)

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
- **D 採用 (2026-09-27、再開の trigger を満たしたため)**: global が 3 日で 117.1k → 126.0k に増え、obaket の見積もりが約 150.3k に達した。
  `paths:` ではなく hook の注入へ移した: 3 本を `_claude/rules/` から `_claude/issue-rules.d/` へ `git mv` し、SessionStart hook
  `issue-rules-inject.sh` が 1 本ずつ注入する (issues/ を持つ repo だけ)。詳細は下の「D の実施」

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


### D の実施 (2026-09-27)

- 🚨 **hook 1 本の注入は約 10k 字で末尾が黙って落ちる** (実測: haiku に末尾の目印を答えさせた。9,525 字は届き、11,925 字は先頭の目印だけ。
  9.5k 字の hook 2 本は両方届いた = 上限は呼び出しごと)。issue-rules.md (4.8k) に 3 本 (9.6k) を足すと 1 本では届かないので、
  `issue-rules-inject.sh issue-rules.d/<x>.md` の形で 1 ファイル 1 呼び出しにし、settings.json に 3 本足した。
  本文が 9,500 字を超えたら先頭に「Read せよ」の警告を出す (先頭は打ち切られない)
- `~/.claude/CLAUDE.md`「レビュー方針」が一般のレビューの作法を `issue-creation-codex-review.md` の代替節に預けていた
  (issues/ の無い repo でも使う) ので、作法 (観点を分けて反証させる) を「レビュー方針」へ移し、issue 側は参照だけにした
- 常時読み込みの rule から 3 本を指していたリンク (verify-design-intent / codex-drive / hook のコメント / Makefile / rationale) を張り直した
- 検査: `tests/claude/test_issue_rules_inject.sh` に ① 各ファイルが引数で全文注入される ② 上限を超えない ③ settings.json の配線と
  `issue-rules.d/` が 1 対 1 ④ 上限超えで先頭に警告、を足した。変異で red を確認: 配線を 1 本消す / 警告の if を外す /
  引数を無視する / 上限を 3,000 字に下げる (2 本が NG)
- 字数 (`wc -m`、`paths:` の無いファイルだけ、UTF-8):

| | 前 (今日の master) | 後 |
|---|---|---|
| global の rule | 113.2k | 103.6k |
| `~/.claude/CLAUDE.md` | 12.8k | 12.8k |
| global 計 | 126.0k | **116.4k** |
| dotfiles のセッション (repo 層 8.5k) | 134.5k | **124.9k** |
| obaket のセッション (repo 層は起票時の 24.7k。見積もり) | 150.7k | **141.1k** |

- 取りこぼし: `move-report-conclusions-to-issues.md` の発動点 (`./tmp` にレポートを書き出した) は issues/ の無い repo でも起きるが、
  そこでは届かなくなる。移し先の半分 (コードのコメント) は常時読み込みの `pending-issue-rationale-in-code.md` と
  CLAUDE.md「一時ファイルの配置」が持っているので受け入れた
- 注入の確認 (push + `~/dotfiles` pull の後。`claude -p --model haiku --setting-sources '' --settings <SessionStart だけの settings>`。
  Stop hook を外さないと応答が差し戻しで上書きされる): 3 本の本文にしか無い語 (`ccd_session_mgmt` / `prompt_issue_NNN.md` /
  `probe-dex6.js`) が、`~/dotfiles` では 3 つとも YES、issues/ の無い dir では 3 つとも NO。通常の user 設定で issues/ の無い dir を
  起こすと、常時読み込みの rule の語 (`poing`) は YES、3 本の語は NO (常時読み込みから外れた)
- `~/.claude/rules/` に残った 3 本の古い link (移動元を指す dangling) は、指し先が `~/dotfiles/_claude/rules/<同名>` で実在しないことを
  確かめてから、その 3 本だけ消した (他のマシンでは次の `./setup.sh` が消す)
- `make test` は rc=2。落ちたのは 3 本で、うち `test_issue_links_valid.sh` (過去の issue 17 本が旧パスをリンクしていた) は張り直して緑。
  残る 2 本は今回の変更と無関係で、master でも同じように落ちる: `test_issue_numbers_unique.sh` (番号 563 の重複) /
  `tests/nvim/test_cheatsheet.sh` (このマシンに tree-sitter の parser が揃っておらず、実行のたびにダウンロードの表示が混ざる)
### 残タスク

- [x] obaket のセッションの常時読み込み合計の実測 (2026-09-28。下の「obaket の実測」)
- 2026-09-25（dotfiles-7d）: A を戻した（commit「docs(claude): mutation-verify の paths: を外し、無条件の読み込みに戻す (issue 414)」）。
  B だけでは obaket の見積もりが 141.8k で、上限までの余裕は約 8k しか戻っていない。D（issue 運用の 3 本 9.5k を hook 注入へ寄せる）を
  検討する時期は近い

### obaket の実測 (2026-09-28)

checkout が無くても測れる: `gh api repos/jiikko/<repo>/contents/<path>?ref=<sha>` で `CLAUDE.md` と `.claude/rules/*.md` を取り、
`paths:` の無いファイルだけを `LC_ALL=en_US.UTF-8 wc -m` で数えた (clone はしていない)。my-products は master `6223aac0`、
obaket はその submodule の commit `92c83eb`。

| 層 | 字数 |
|---|---|
| global (`_claude/rules` 34 本中 `paths:` 無しの 30 本 + `~/.claude/CLAUDE.md`。dotfiles `cace043a`) | 116.4k |
| my-products umbrella (`CLAUDE.md` + `.claude/rules` 13 本中 `paths:` 無しの 5 本) | 13.6k |
| obaket (`CLAUDE.md` + `.claude/rules` 19 本中 `paths:` 無しの 4 本) | 13.3k |
| **obaket のセッション** | **143.3k** (上限 150k まで約 6.7k) |

- 見積もり (141.1k) より repo 層が 2.2k 多い (起票時は 24.7k、今は 26.9k)
- `wc -m` の合計であって、Claude Code の警告の値そのものではない (起票時は警告の値と `paths:` 無しの合計がほぼ一致した)
- 余裕は約 6.7k で、global の流入は 3 日で約 9k だった (9/25 の 117.1k → 9/27 の 126.0k)。**再開の trigger**: obaket の起動で
  上限超えの警告が再び出たとき、または global が 123k を超えたとき


### 追補 (2026-10-06): 再び上限を超えたので、paths: 以外の手で 7.4k 字を戻す

obaket のセッションで `⚠ 42 instruction files add up to 152.9k chars` が出た (global 120.5k + my-products 14.1k + obaket 14.4k + obaket/macOS 4.4k)。

- **mutation-verify は paths: にせず、チェックリストだけを切り出した**: 「よくある『守っていないテスト』の形」節 (6.5k) を
  `_claude/rules-checklists/mutation-verify-new-tests.md` (起動時には読まれない) へ移し、rule には「変異を当てる前に Read する」の指示を残した。
  発動点・手順・ルールは常時読み込みのまま (上の「A 却下」の理由を壊さない)。rule は 13.2k → 6.9k
- **`_claude/CLAUDE.md`「設計方針」の発動点→rule の索引表から、常時読み込みの rule を指す 11 行を外した** (rule 本体が毎回全文読まれるので重複)。
  `paths:` 付きの zsh-hook の行だけ 1 文で残した。13.2k → 12.0k。「スキルファイル参照」表は `tests/claude/test_skill_trigger_table.sh` が守っているので残した
- 結果 (`wc -m`、`paths:` の無いファイルだけ): global 120.5k → **113.1k**。obaket のセッションの見積もりは 153.4k → 約 146.0k
- 反証レビュー (sonnet 1 体、read-only): 採用 2 件 = `avoid-wall-clock-assertions.md` の「あちらの節に」の参照を書き換え /
  `tests/CLAUDE.md` の写像の既知の穴に `rules-checklists` を足した。却下 2 件:
  - 「Read するかは自己申告で、忘れても機械は止めない」: paths: でも同じく Write→commit では読み込まれない (上の A 却下)。
    検査で止められるのは参照先ファイルの存在までで、読んだかどうかは止められない。**再開の trigger**: チェックリストの項目に当たる見落としが、
    Read されなかったせいだと分かる実例が出たとき
  - 「`~/dotfiles/...` の絶対パスは clone の位置が違う環境では辿れない」: `rules-rationale` の参照と同じ前提 (`~/.claude/rules/` の link 先も `~/dotfiles`)

### 追補 (2026-10-12): 再び上限を超えたので、checklist の切り出しを 2 本に広げて 10.4k 字を戻す

obaket のセッション (`macOS/` で起動) で `⚠ 42 instruction files add up to 152.1k chars` が出た。

- **`adversarial-review-own-safeguards.md`**: 「## やること」の §0〜§8 の本文を逐語のまま `_claude/rules-checklists/adversarial-review-own-safeguards.md` へ移した。
  rule には発動点・ルール・やらないことと、「発動したら Read する」の指示と節の見出しの目次を残した。11.4k → 2.4k
  - 節番号は振り直していない。hook・テスト・scripts・Makefile・他の rule のコメントが「§8」「0-A」のように番号で参照していて (約 20 箇所)、
    rule の目次からたどれるので参照は書き換えていない
- **`commit-with-pathspec.md`**: 「履歴操作 (reset / amend / rebase)」節の本文を `_claude/rules-checklists/commit-with-pathspec.md` へ移した。
  rule には Read の指示、`reset HEAD~1` が並行セッションの commit を切り落としうること、確認用の `git log -N` の 1 文を残した。7.0k → 5.6k
- 逐語の確認: 消した行と checklist の本文行を空行を除いて diff し、一致した (adversarial は相対リンク 1 本を `../rules/` 付きに直した差だけ)
- **`paths:` は付けなかった**: 常時読み込みの 30 本はどれも発動点が行動 (commit・テスト・tmux・検証) で、上の C 却下と dotfiles の `CLAUDE.md` の規約のとおり。
  ユーザーに確認した上で見送った (2026-10-12)
- 結果 (`LC_ALL=en_US.UTF-8 wc -m`、`paths:` の無いファイルだけ): global 117.9k → **107.5k**。obaket のセッションの warning の値は 152.1k → 約 141.7k の見込み (起動し直しての実測は未確認)
- 反証レビュー (sonnet 1 体、read-only): P1 なし。採用 1 件 = commit-with-pathspec の rule 側から「他人の commit を切り落とす」危険と確認コマンドが消えていたので 1 文で戻した。記録 2 件 (直さない):
  - P3「trigger の動詞の列挙に `git pull --rebase` や `push ${SHA}:ref` の別名が入らない」: 列挙の rebase / refspec で拾える範囲で、やること節の「reset / amend / rebase の前に git log で確認する」も常時読み込みに残っている
  - P3「他の rule の『§1.5』『0-B』の参照は rule の目次を経由して 1 段多くたどる」: 実害は小さく、約 20 箇所を書き換える差分の方が大きい
- **再開の trigger**: obaket の起動で上限超えの warning が再び出たとき。次の候補は `verify-execution-not-just-exit-code.md` (7.9k) の参照資料の節 (「確認の仕方」の表など)。
  検証のたびに発動する rule なので、手順の節は切り出さない

### 追補 (2026-10-30): 再び上限を超えていたので 19.4k 字を戻し、上限を機械で検査する

- **超えていた**: global 層 126.4k (`_claude/CLAUDE.md` 16.9k + rule 109.4k)。obaket の `macOS/` で起動するセッションは、repo 層 38.7k
  (gh api で測った。my-products 14,089 + obaket 19,573 + obaket/macOS 5,011) を足して約 165.1k。dotfiles のセッションは 140.1k
- **縮めた** (commit「docs(claude): 常時読み込みの rule を 109.4k → 90.0k 字に縮める」): ルール節の言い換えの `## やること / やらないこと` を消し
  (ルール節に無かった 10 項はルール節へ移した。adversarial-review-own-safeguards の節は checklist へ切り出した規範の唯一の常時読み込みなので残した)、
  `## 関連` を発動点の違う姉妹ルールと一次情報だけにし、`## なぜ` の定型文を縮め、本文の実例の括弧書き 45 件を各 rationale の「本文から移した実例」節へ移した
- 結果 (`LC_ALL=en_US.UTF-8 wc -m`、paths: の無いもの): rule 109,408 → 約 90.1k。global 層 107.1k、dotfiles のセッション 120.8k、obaket の見込み約 145.8k
- **検査**: `scripts/check_instruction_budget.sh` (`make test-lint` の `test-instruction-budget`、CI の Lint)。global 層を 150,000 − 39,000 (他の repo 層の取り置き) 以内に、
  dotfiles のセッションを 150,000 以内に保つ。取り置きは CI から他の repo を読めないので定数。**再開の trigger**: 他の repo で上限超えの警告が出たら、repo 層を測り直して取り置きを上げる
- 反証レビュー (opus 1 体、読み取りのみ): P1 なし。採用 = ブランチの削除の禁止が圧縮で消えていた / rules のサブディレクトリを数えていなかった / 境界のテスト / perf と tmux の項の欠け / 空行の残り。
  記録 = 出典の指し先 4 件 (issue 098 等) は rationale へ移していない (規範ではない)・`paths:` の値と `@import` の展開は数えない (検査の冒頭に書いた)
- 未確認: auto memory (MEMORY.md) が警告の集計に入るか。入るなら、obaket の memory の分だけ取り置きが足りない
- 2026-10-30 追記: `paths:` の rule は 2.1.314 では一致するファイルの Read・Write・Edit で読み込まれる (headless の A-B で実測。`_claude/skills/claude-md-compact/paths-probe.sh`)。
  上の「A 却下」「D 見送り」の根拠のうち「Write / Edit では発火しない」は今の版では成り立たない。commit・テストの実行のようにファイルに触れずに進む行動が発動点の規範は、引き続き無条件に置く
- 2026-10-30 追記 (2): `move-report-conclusions-to-issues.md` を `issue-rules.d/` から `_claude/rules/` へ移し、`paths: tmp/** **/tmp/**` を付けた (D 案)。issues/ のある repo では hook が起動時にも注入し続ける。
  user-level の rule の照合を headless で実測 (2.1.314・issues/ の無い使い捨ての repo): 何もしない NONE / `tmp/report.md` の Write で読み込まれた / `notes/` は NONE / `sub/tmp/` は読み込まれた / project の外の `…/tmp/` を絶対パスで Write は NONE。
  よって worktree で作業するセッションが本体 checkout の tmp に書いても読み込まれない (issues/ のある repo は hook で補われる)
