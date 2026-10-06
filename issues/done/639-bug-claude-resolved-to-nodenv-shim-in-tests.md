# 639 (bug): test_claude_mods.sh が nodenv の shim を claude と取り違えて落ちる

起票日: 2026-10-06

## 概要

`make test` で `tests/claude/test_claude_mods.sh` だけが落ちる。`command -v claude` が nodenv の shim
(`~/.nodenv/shims/claude`) を「見つかった」と返し、その shim が実行時に失敗する (rc=127) ため。
本物の Claude Code (`~/.local/bin/claude`) は PATH の後ろにあって届いていない。

このテストは「手元で claude が見つからなければ skip せず失敗」にする設計 (冒頭のコメント)。
見つからないのではなく**違うものを見つけている**ので、その設計の意図 (手元では必ず検査する) も果たせていない。

## 詳細

実測 (2026-10-06、Claude Code 2.1.288):

```
$ which -a claude
/Users/koji/.nodenv/shims/claude
/Users/koji/.local/bin/claude
$ nodenv version
22.14.0 (set by /Users/koji/.nodenv/version)
$ ~/.nodenv/shims/claude --version; echo $?
127        # nodenv: claude: command not found / exists in these Node versions: 24.2.0
$ ~/.local/bin/claude --version
2.1.288 (Claude Code)
```

`make test` の出力 (mods ごとに validate と test の両方が落ちる):

```
✗ canary: claude plugin validate が失敗
nodenv: claude: command not found
✗ canary: claude plugin test が失敗 (rc=127, 実行件数=読めない)
(desktop-statusline・issue-band も同じ)
```

shim が残っているのは、Node 24.2.0 に npm 版の `claude` が入っているため (nodenv は、どれかの版に入っている
コマンドの shim を作る)。今の版 (22.14.0) で呼ぶと「この版には無い」で終わる。

## 対応方針

- **テスト側 (本命)**: `claude` を PATH の名前で決めず、**実際に動く実体**を選ぶ。`which -a claude` の候補を順に
  `--version` で確かめ (stderr は捨てる。shim は失敗時に nodenv の案内を出す)、最初に成功したものを使う。
  候補は 1〜2 個なのでコストは小さい
  - 全滅したときの扱いを決める: 手元は従来どおり失敗。**CI は「見つからない」と同じく skip (exit 77) にするか**
    (CI に shim だけがある状況は今は無いが、`command -v` が非空でも動かない場合を明示的に扱う)
  - pro-con の解決 (issue 464、`src/pro-con/dispatcher/claude.go`) は**この状況では使えない**。shim と判定すると
    `nodenv which claude` に差し替えるが、今の版に claude が無いのでそれも rc=127 で失敗し、後ろの
    `~/.local/bin/claude` には落ちない (2026-10-06 実測)。Go と bash で共有する経路も無い。同じ問題が pro-con 側にも
    あるかは別に確かめる (pro-con は cwd の版で選ぶ意図があるので、同じ直し方が正しいとは限らない)
- **同じ形の箇所**: `bin/skill-eval` の `CLAUDE_BIN="${SKILL_EVAL_CLAUDE:-$(command -v claude || true)}"` も shim を選ぶ。
  `-x` の検査は shim でも通るので、起動時ではなく実行時に 127 で落ちる。`tests/bin/test_skill_eval.sh` は偽の claude を
  使うので `make test` には出ず、実害は skill-eval を実際に走らせたときだけ (`SKILL_EVAL_CLAUDE` で回避できる)。
  `tests/zshrc/ai-commands/test_ai_commands.sh` は存在を見るだけなので対象外
- **環境側 (手元の応急)**: Node 24.2.0 の npm 版 `claude` を消せば shim が消え、今の PATH でも本物に届く。
  ただしこれはこの Mac の状態を直すだけで、別の版管理の shim で再発するので、テスト側の修正の代わりにはしない

## 反証レビューの結果 (2026-10-06、read-only のサブエージェント 1 本)

- 採用: pro-con の解決を共有できるという当初の見立ては誤り (上の方針に反映)。skill-eval の実害の範囲。CI で全滅したときの扱い
- 却下: 「issue 464 / 618 が見つからない」→ `issues/epic/415/done/` と `issues/epic/618/` に在る (`ls` で直下だけを見ていた)

## 関連ファイル

- `tests/claude/test_claude_mods.sh` (`claude_bin="$(command -v claude)"` の行)
- `bin/skill-eval` (`CLAUDE_BIN=` の行)
- `src/pro-con/dispatcher/claude.go` (shim を辿る既存の解決)
- issue 464 (pro-con の同じ問題)、epic 618 (mods)

## 進捗

- [x] テスト側で実体を選ぶ — `bin/lib/claude_bin.sh` の `resolve_claude` を新設。PATH の候補 (`type -aP`) を順に
  `--version` で試し、最初に動いたものを返す。rc は 0 = 選べた / 1 = 無い / 2 = 在るが動かない
- [x] `tests/claude/test_claude_mods.sh` を `resolve_claude` に置き換え。CI で skip するのは rc=1 (無い) だけで、
  rc=2 (壊れた claude しか無い) は CI でも失敗にする (緑の skip に隠さない)
- [x] `bin/skill-eval` も `resolve_claude` に置き換え (`SKILL_EVAL_CLAUDE` を渡したときは従来どおりそれを使う)
- [x] 回帰テスト `tests/bin/test_claude_bin.sh` (6 件)。本物の claude には触らない (PATH を偽の dir と /usr/bin:/bin だけにする)
- [x] 変異検証: (1) `--version` の確認を外す (= 修正前の `command -v` と同じ動き) → 「shim が先」「shim だけ」が red
  (2) 候補の起動で stdin を閉じない → 「stdin を読む候補が先」が red (3) 重複の除去を外す → 「重複」が red。
  いずれも狙ったケースだけが落ちた
- 結果: 手元で `test_claude_mods.sh` が通る (mods 3 件の validate / test)。修正前はこの環境で毎回 rc=127 で落ちていた

### 敵対レビュー (2026-10-06、read-only のサブエージェント 1 本、bash 3.2 で実測)

- 採用: 候補が stdin を読むとループの入力 (候補の一覧) を食って後ろの本物に届かない (再現済み → `</dev/null`)。
  CI で「在るが動かない」まで skip になる (→ rc を 1 / 2 に分けた)。PATH の重複で同じ候補を 2 回起動する (→ 除く)
- 採用 (後から): `--version` に時間の上限が無い → 既定 10 秒の上限を付けた (環境変数 `CLAUDE_BIN_VERSION_TIMEOUT`)。
  macOS に標準の timeout が無いので、背景で起動して 0.1 秒ごとに見る形 (`tests/bin/test_go_autobuild_warmup.sh` の
  `runs_within` と同じ)。上限を超えた候補は kill して「応答なし」として次へ進む。テストに「応答しない候補が先」
  「打ち切った候補が残らない」「応答しない候補だけ」の 3 件を足し、打ち切りを無効にする変異で前後 2 件が red になることを確かめた
- 記録のみ: PATH の相対パスの要素では相対パスが返る (普通の環境では起きない)
- 却下: `bin/skill-eval` を symlink 経由で呼ぶと lib が見つからない → 同じファイルの ROOT も同じ前提
  (`BASH_SOURCE` の dirname) で、今回の変更で生まれた制約ではない
- 修正後の 2 周目のレビューは回していない (各修正は変異で直接 red を確かめた)
