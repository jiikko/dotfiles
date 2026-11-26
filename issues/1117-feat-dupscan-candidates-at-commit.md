# 1117 (feat): 新しい関数の候補 (dupscan) を commit の時点でも出し、Bash で関数を足す経路を塞ぐ

起票日: 2026-11-27

## 概要

新しい関数を書く直前に、名前の近い既存の定義を出す hook (`_claude/hooks/inject-dupscan-candidates-on-new-func.sh`) は Write / Edit にしか掛からない。
Bash (python / sed / heredoc) で足した関数は見ないので、同じ役割の関数をもう 1 つ書いても何も出ない (hook の冒頭の「検出しない形: Bash の書き込み」)。

## 詳細 (起きたこと)

vlc-multi-video-player の作業 (2026-11-26) で、打ったパスの読み方 (`FavoritesTextFormat.typedPath`) を python のスクリプトで足した。
同じ規則は `TypedPathResolver` (サイドバーの入力欄) が既に持っていて、読み方が 2 か所に分かれた (vlc-multi-video-player の issue `unify-typed-path-reading`)。

- Bash で書いたので hook は走らなかった
- `dupscan index -name typedPath` を手で引くと `SidebarViewModel.openTypedPath` (サイドバーの入口) と `ToastMessages.typedPathNotFound` が出る。
  hook の照合 (名前が等しいか前方が一致) では `typedPathNotFound` だけが出る (`typedPath` が前方)。`openTypedPath` は後ろに含む形なので出ない
- 本命の `TypedPathResolver.resolve` は名前が違うので、名前の照合では出ない (これは定期の監査 `/audit` の duplication / entry-parity で拾う)

## 対応方針

A. **commit の時点でも候補を出す** (止めない。additionalContext だけ)
   - PreToolUse(Bash) の新しい hook。`lib/git_cmd_detect.sh` の `git_cmd_invokes "$cmd" commit` が真のときだけ走る。見る repo は cwd と、`cd` / `-C` の行き先 (git-state-verify.sh と同じ扱い)
   - 対象のファイル: commit の行の `--` の後のパスがあればそれ、`-a` / `--all` なら追跡しているファイルの変更すべて、どちらも無ければ stage したもの。`.go` / `.swift` で git が ignore しないもの
   - HEAD のファイル (無ければ空) と、作業ツリーのファイルを比べ、新しく増えた関数・型を引く
   - 判定の本体 (新しい関数の見つけ方・名前の照合・予算・1 セッション × 名前で 1 回の印・計測のログ) は、今の hook の python を `_claude/hooks/lib/dupscan_inject.py` に移して共有する。
     写すと照合が 2 つになり、片方だけ直す形になる。印を共有するので、Edit の時点で出した名前は commit で繰り返さない
   - 計測のログ (`claude-dupscan-inject.log`) に入口 (edit / commit) の列を足し、`scripts/dupscan_usage_report.py` で commit の時点の注入を数える
C. **共通の CLAUDE.md の「Bash (python / sed) でファイルを編集するなら」の項に、Write / Edit に掛かる hook も通らないことを足す**。関数・型を足す編集は Edit / Write で行う

見送り: B (名前の照合を「後ろに含む」まで広げる)。候補が増えて読み流されるかを測ってから決める。A の計測のログで、commit の時点の候補の数と、その後の探索の有無を見てから再提起する

## 関連ファイル

- `_claude/hooks/inject-dupscan-candidates-on-new-func.sh`・`_claude/hooks/lib/git_cmd_detect.sh`・`_claude/hooks/git-state-verify.sh` (cwd と `-C` の扱いの手本)
- `_claude/settings.json` (PreToolUse の Bash)
- `tests/claude/test_inject_dupscan_candidates_on_new_func.sh`
- `scripts/dupscan_usage_report.py`・`docs/dupscan-overview.md`・`_claude/skills/dupscan/SKILL.md`・`src/dupscan/README.md` (hook を説明している入口)
- `_claude/CLAUDE.md` (C)

## 進捗

- [ ] 判定の本体を lib へ移す (今の hook のテストが緑のまま)
- [ ] commit の時点の hook とテスト (変異で赤を見る)
- [ ] 計測のログの入口の列と report
- [ ] C: CLAUDE.md
- [ ] 入口の文書 (docs/dupscan-overview.md・skill dupscan・src/dupscan/README.md)
- [ ] push して本体を pull し、新しいセッションで注入を観測する
