# 1053 (chore): hook の点検 (2026-11-10) で残った指摘 — tmux の kill の判定に残る素通りと時間の余裕、$TMPDIR に溜まる dupscan の印ほか

起票日: 2026-11-10

## 概要

`_claude/hooks/` の 28 本を点検した。テスト・shellcheck を回し、何もしない入力での所要を実測し、opus の反証レビューを 1 本、直した差分への敵対的レビューを 2 周通した。
再現した不具合 3 件とテストの穴は同じ日に直した (下の「直したもの」)。この issue は、直さずに残した指摘の行き先を持つ。

## 残タスク

- [ ] **`deny-bare-tmux-kill.sh` は、引用符の中の `|`・`&&`・改行でもセグメントを割る** (旧版からの穴。2026-11-10 の修正の後も残る)
  - 割った後半が `#` で始まると、行頭コメントとして丸ごと捨てられる。そのため `<tmux> list-panes -F '#{a}|#{b}'; <tmux> kill-server` (`&&`・改行も同じ) が素通りする
  - 直すなら、連結演算子での分割 (`sed -E 's/&&|\|\||[;|]/\n/g'`) を、引用符を追う形にする。分割と unquote を 1 パスにまとめると、時間の余裕 (下) も悪化しない
- [ ] **`deny-bare-tmux-kill.sh` は、和文の大入力で 10 秒の timeout を越え、何も出さずに通る (fail-open)。旧版も同じ**
  - 約 8000 字の和文を引用符で囲み、末尾に ` kill-session'` を付けた `echo` の行を 8 本並べ、最後に kill-server を置く。この入力で新版は rc=124 (10.16 秒)・stdout 空になった (2026-11-10、反証レビューの実測)
  - 4 行なら 5.58 秒で deny。`unquote` 1 回の所要は旧版 1.38 秒・新版 1.39 秒で同じ
  - 別の形 (和文 8000 字の `echo kill-s …` を 4 行 + kill-server) では、旧版 6〜11 秒・新版 7 秒かかった (2026-11-10、交互に 2 回ずつ)。どちらも deny した
  - 67 行目のコメント「8KB は実測で 100ms 未満」は今の環境では成り立たない (ASCII 8000 字で 0.24 秒)
  - settings の timeout は 10 秒で、越えると出力 0 byte = 素通りになる
  - 既存の性能テスト (`big_payload`) は `;` で繋いだ 1 本の巨大なセグメントなので MAX_UNQUOTE_BYTES を越えて 1 文字ループを飛ばし、この形 (上限未満のセグメントが多数) を通らない
  - `${#seg}` は ja_JP.UTF-8 では文字数を数え、マルチバイトでは `${s:i:1}` がさらに遅い。直すなら全セグメントの合計に予算を置くか、1 文字ループを awk へ移す
- [ ] **`deny-bare-tmux-kill.sh` が tmux の語として拾わない形** (旧版も新版も allow。2026-11-10 の反証レビューで再現)
  - サブシェルとコマンド置換: `(<tmux> kill-server)`・`echo $(<tmux> kill-server)`・バッククォート。語が `(tmux` / `$(tmux` になり、scan_segment の `case` に当たらない。冒頭の「原理的な限界」にも載っていない
  - 語の途中のバックスラッシュ・引用符: `t\mux kill-server`・`<tmux> kill-se\rver`・`<tmux> 'kill-server'` (サブコマンドを引用符で囲むと Q に潰れる)・`bash -c $'<tmux> kill-server'` (`$'` で -c の照合が外れる)
  - 直すなら、語ごとに `\` と空の引用符を除いてから照合し、`(`・`$(`・`` ` `` を語の区切りに数える
  - `ssh h '<tmux> kill-server #x'` は旧版では deny、新版では allow。旧版の deny は、引用符の途中で切ったために中身が残った偶然による。ssh の先は手元の本番のサーバではないので、塞ぐかどうかは判断が要る
  - 偽陽性の例を 1 つ確かめた: `"\tmux" kill-server` は deny になる (二重引用符の中の `\` が次の文字を区別なく飲み込む。shell ではこの形は実行されない)
- [ ] **$TMPDIR に session ごとの印が溜まる** (2026-11-10 に `ls $TMPDIR | grep -c '^claude-dupscan'` = 73 件。最古は 11/3)
  - `suggest-dupscan-for-go-edit.sh` (`claude-dupscan-go-edit-<sid>`: 8 件) と `suggest-dupscan-for-symbol-grep.sh`
    (`claude-dupscan-used-` 12 件・`-go-nudge-` 15 件) は、冒頭で「OS が片付ける」として 1 session 1 ファイルを書く。macOS の $TMPDIR は数日では片付かない
  - `inject-dupscan-candidates-on-new-func.sh` は issue 1036 で同じ理由から `${XDG_CACHE_HOME:-~/.cache}/claude-dupscan-inject.seen` の
    1 ファイル (上限つき・古い半分を捨てる) へ移した。同じ形の 2 本はまだ移っていない。移すなら 3 本の印の読み書きを 1 つの lib にまとめる (写しを 3 つにしない)
  - 移す前の inject の印 (`claude-dupscan-inject-<sid>-<名前>`: 38 件) は移行の際に消されずに残っている。消すかどうかはユーザーが決める (`cleanup-tmp-dir` skill)

## 確かめたが直さなかったもの (理由)

- **Bash の PreToolUse 9 本を 1 本の dispatcher にまとめる**: 採らない
  - 何もしない入力 (`ls -la`) での所要は 1 本 12〜40ms (`deny-tmp-root-write` の 39.7ms が最大。2026-11-10 に 7 回測った中央値)
  - 同じイベントの hook は並列に走るので、待ちは最も遅い 1 本で決まり、まとめても縮まない。まとめると hook ごとの fail-open (1 本が落ちても他は効く) とテストの独立を失う
  - 再提起するなら、並列でなく直列に走っていることを実測で示す
- **`deny-tmp-root-write.sh` が `Write|Edit` に配線されている (Edit では何もしない)**: 直さない
  - Edit を止めないことは `tests/claude/test_deny_tmp_root_write.sh` が明示して検査していて、hook 冒頭の「検出しない形」にも書いてある
  - 外しても、Edit 1 回あたり bash の起動が 1 本 (約 20ms・並列) 減るだけ
- **`next-claim-push.sh` / `next-claim-unshared.sh` の `git log HEAD --branches --not --remotes`**: 直さない
  - 今いない別ブランチの claim も未 push として拾うための母集合 (issue 311 の決定)
  - 311 の対応表は git-state-verify も `HEAD --branches` にしていた。git-state-verify だけは用途 (今 push したものが載ったか) が違うので、その部分を覆して直した (下)

## 未確認のリスク (再現していない。観測したら起票する)

- **hook の `git status` は `--no-optional-locks` 無しで走る**
  - `issue-progress-check.sh` は Stop のたびに全 worktree で、`next-claim-unshared.sh` は毎プロンプト打つ
  - index.lock を一時的に取るので、並行 session の `git add` / `commit` が「index.lock: File exists」で落ちうる。実害は観測していない
  - 観測したら `GIT_OPTIONAL_LOCKS=0` を hook の共通の入口 (lib) で立てる
- **tmux の状態表示が実態とずれることがある**
  - Stop で `issue-progress-check.sh` が block を返して作業が続いても、同じ Stop の `tmux-pane-state.sh idle` は「✓ idle」とベル・通知を出す
  - SessionStart の compact でも `start` が走り、idle 表示に戻る
  - ずれは次のツール呼び出しまで続く
- **`normalize-settings.sh` の書き戻しは原子的でない**
  - 書き戻しは `cat >` (symlink を壊さないため) で、truncate から書き終わりまでの間に別 session が空の settings.json を読む窓がある
  - 原子的にするなら、`readlink -f` の実体と同じ dir に一時ファイルを作り、実体の上へ `mv` する
- **next/ の列挙が lib と別に写されている**
  - `next-claim-push.sh` と `next-claim-unshared.sh` が、next/ の列挙を lib (`lib/issue-hooks.sh` の `issue_hook_resolve_dir`) と別に写している
  - 除外 (node_modules など) の有無と、単数の `issue/` の扱いが写しの間で違う。その形の repo は手元に無い
- **継承した `GIT_DIR` / `GIT_WORK_TREE` を外していない hook がある**
  - 外しているのは `deny-tmp-root-write.sh` と `deny-soft-reset-to-non-ancestor.sh` (`git_location_env_unset`) だけで、git を呼ぶ他の hook (`issue-progress-check.sh` など) は外さない
  - Claude Code が hook にこの env を渡すかは確かめていない
- **interrupt・timeout・PreToolUse の deny でも `PostToolUseFailure` が来るかは確かめていない**
  - 来るなら、git-state-verify の前置き「コマンドは失敗した (rc≠0)」は不正確になる

## 直したもの (2026-11-10)

- **`deny-bare-tmux-kill.sh`: 引用符の中の ` #` から後ろをコメントとして落とし、kill が素通りしていた**
  - 例は、書式文字列 `'#{a} #{b}'` の後ろの `; <tmux> kill-server`
  - コメントの除去を `unquote` の 1 パスへ移し (引用符の外の「空白 + #」だけ)、バックスラッシュも扱う (`\"` で引用符を閉じない)
  - 1 本目の直し方 (別のループ) は、敵対的レビューで「所要が 2 倍になり timeout で素通りする」「タブの後の # で回帰する」と壊されたので捨てた
  - 2 周目のレビューで次の 2 つが出たので、同じ変更で直した
    - 回帰: `/bin/bash -c "… #x"` と `eval "… #x"` が deny から allow に変わっていた。パスの付いたシェルと eval も `-c` と同じに扱う
    - 新しいコードの誤り: エスケープした空白 (`a\ #b`) の後の `#` をコメントとして切っていた
- **`git-state-verify.sh` / `next-claim-push.sh`: rc≠0 で終わった Bash では PostToolUse が走らない**
  - 失敗した commit / push のあとに黙っていた。失敗した Bash で PostToolUse が走らないことは、反証レビューが transcript を集計して報告した (集計の出典は残っていない)
  - `PostToolUseFailure` が失敗した Bash で発火し、入力に `tool_input.command` が入り、返した additionalContext が届くことは、Claude Code 2.1.321 の headless で実測した
  - 両方を `PostToolUseFailure` にも配線し、返すイベント名を入力に合わせた (`lib/hook_io.sh` の `hook_event_name`)
- **`git-state-verify.sh`: 未 push の一覧に今いない別ブランチの commit が混ざっていた**
  - ~/dotfiles で `HEAD --not --remotes` は 0 件なのに、hook の式では 21 件出た。このため「push 済み」が出なかった
  - HEAD の分だけを並べ、別ブランチは件数だけを出す形にした
- **テストの穴**
  - commit した `_claude/settings.json` が `normalize-settings.sh` の不動点かを検査する。崩れた形を commit すると、本体の checkout が dirty になり pull が止まる
  - `gofmt-on-edit.sh` のテストを新設した
  - PostToolUseFailure の配線と、返すイベント名をテストで固定した
