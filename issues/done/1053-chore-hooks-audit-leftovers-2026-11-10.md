# 1053 (chore): hook の点検 (2026-11-10) で残った指摘 — tmux の kill の判定に残る素通りと時間の余裕、$TMPDIR に溜まる dupscan の印ほか

起票日: 2026-11-10

## 概要

`_claude/hooks/` の 28 本を点検した。テスト・shellcheck を回し、何もしない入力での所要を実測し、opus の反証レビューを 1 本、直した差分への敵対的レビューを 2 周通した。
再現した不具合 3 件とテストの穴は同じ日に直した (下の「直したもの」)。この issue は、直さずに残した指摘の行き先を持つ。

## 残タスク

- [x] **`deny-bare-tmux-kill.sh` は、引用符の中の `|`・`&&`・改行でもセグメントを割る** (旧版からの穴。2026-11-10 の修正の後も残る)
  - 割った後半が `#` で始まると、行頭コメントとして丸ごと捨てられる。そのため `<tmux> list-panes -F '#{a}|#{b}'; <tmux> kill-server` (`&&`・改行も同じ) が素通りする
  - 直すなら、連結演算子での分割 (`sed -E 's/&&|\|\||[;|]/\n/g'`) を、引用符を追う形にする。分割と unquote を 1 パスにまとめると、時間の余裕 (下) も悪化しない
- [x] **`deny-bare-tmux-kill.sh` は、和文の大入力で 10 秒の timeout を越え、何も出さずに通る (fail-open)。旧版も同じ**
  - 約 8000 字の和文を引用符で囲み、末尾に ` kill-session'` を付けた `echo` の行を 8 本並べ、最後に kill-server を置く。この入力で新版は rc=124 (10.16 秒)・stdout 空になった (2026-11-10、反証レビューの実測)
  - 4 行なら 5.58 秒で deny。`unquote` 1 回の所要は旧版 1.38 秒・新版 1.39 秒で同じ
  - 別の形 (和文 8000 字の `echo kill-s …` を 4 行 + kill-server) では、旧版 6〜11 秒・新版 7 秒かかった (2026-11-10、交互に 2 回ずつ)。どちらも deny した
  - 67 行目のコメント「8KB は実測で 100ms 未満」は今の環境では成り立たない (ASCII 8000 字で 0.24 秒)
  - settings の timeout は 10 秒で、越えると出力 0 byte = 素通りになる
  - 既存の性能テスト (`big_payload`) は `;` で繋いだ 1 本の巨大なセグメントなので MAX_UNQUOTE_BYTES を越えて 1 文字ループを飛ばし、この形 (上限未満のセグメントが多数) を通らない
  - `${#seg}` は ja_JP.UTF-8 では文字数を数え、マルチバイトでは `${s:i:1}` がさらに遅い。直すなら全セグメントの合計に予算を置くか、1 文字ループを awk へ移す
- [x] **`deny-bare-tmux-kill.sh` が tmux の語として拾わない形** (旧版も新版も allow。2026-11-10 の反証レビューで再現)
  - サブシェルとコマンド置換: `(<tmux> kill-server)`・`echo $(<tmux> kill-server)`・バッククォート。語が `(tmux` / `$(tmux` になり、scan_segment の `case` に当たらない。冒頭の「原理的な限界」にも載っていない
  - 語の途中のバックスラッシュ・引用符: `t\mux kill-server`・`<tmux> kill-se\rver`・`<tmux> 'kill-server'` (サブコマンドを引用符で囲むと Q に潰れる)・`bash -c $'<tmux> kill-server'` (`$'` で -c の照合が外れる)
  - 直すなら、語ごとに `\` と空の引用符を除いてから照合し、`(`・`$(`・`` ` `` を語の区切りに数える
  - `ssh h '<tmux> kill-server #x'` は旧版では deny、新版では allow。旧版の deny は、引用符の途中で切ったために中身が残った偶然による。ssh の先は手元の本番のサーバではないので、塞ぐかどうかは判断が要る
  - 偽陽性の例を 1 つ確かめた: `"\tmux" kill-server` は deny になる (二重引用符の中の `\` が次の文字を区別なく飲み込む。shell ではこの形は実行されない)
- [x] **$TMPDIR に session ごとの印が溜まる** (2026-11-10 に `ls $TMPDIR | grep -c '^claude-dupscan'` = 73 件。最古は 11/3)
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

## 残タスクの対応 (2026-11-10)

- **`deny-bare-tmux-kill.sh` の字句解析を書き直した** (残タスクの 1〜3 つ目)
  - 引用符・`\`・行継続・コメント・連結演算子・`(` `)` `$(` `` ` `` を shell の規則で 1 パスで割る awk に移した (LC_ALL=C でバイト単位)。
    引用符は剥がして語に繋ぐので、`'kill-server'`・`t\mux`・`kill-se\rver` も語としては tmux の語になる。引用符の中の `|` `&&` 改行 ` #` は区切りでもコメントでもない
  - 中身が実行される文字列 (`sh|bash|zsh|dash|ksh … -c`・`eval`・二重引用符の中の `$(` / `` ` ``・`$'..'`) は割り直す。heredoc の本文は 1 行ずつ別のコマンドとして割る
    (行をまたぐ引用符で後ろの kill を飲み込まない。本文の散文の偽陽性は前と同じ)
  - 前置きの絞り込みは `\` と引用符を除いた形で `kill-s` を探す (`kill\-server` を素通りさせない)
  - 大きさの上限 (128KB) をバイトで数える (前は ja_JP で字数を数えていて、和文の大入力が上限の内に入っていた)
  - 時間: 和文 8000 字 × 5 行で 0.3 秒 (前は 7.8 秒)、120KB の 1 語で 0.7 秒。書き直しの途中で 2 つの 2 乗のコストを見つけて直した:
    awk の `sub(/.*\//)` を長い語に当てる (24KB の語で 1.1 秒)・bash の `${t##*/}` (24KB の語で 2〜4 秒)。テストは両方を戻すと赤になる大きさにした
  - `ssh h 'tmux kill-server'` は allow のまま (中身は手元の本番でなく相手のマシンで走る。前の版の deny は引用符を途中で切った偶然)。hook の冒頭の「原理的な限界」に書いた
  - `"\tmux" kill-server` は deny のまま (先頭の `\` を剥がして tmux と読む。引用符の外の `\tmux` は alias を避けた本物の tmux なので、剥がす側に倒す)。hook の冒頭に書いた
- **dupscan の hook 3 本の印を 1 つの lib に寄せた** (残タスクの 4 つ目)
  - `_claude/hooks/lib/session_marks.py`: 「session 種類 値」の行を `${XDG_CACHE_HOME:-~/.cache}` の 1 ファイルに lock の下で追記し、64KB を越えたら古い半分を捨てる
    (inject の hook が持っていた読み書きを出した。行の形は前の `claude-dupscan-inject.seen` と同じなので、前の印はそのまま読める)
  - `suggest-dupscan-for-go-edit.sh`・`suggest-dupscan-for-symbol-grep.sh` は `claude-dupscan-hooks.marks` に持つ ($TMPDIR に session ごとのファイルを作らない)
  - inject の hook は lib を import する。import で `hooks/lib/__pycache__` を作らない (`sys.dont_write_bytecode`。本体の checkout に untracked が残る)
  - $TMPDIR に残っている前の印 (`claude-dupscan-*`) は消していない。消すかはユーザーが決める (`cleanup-tmp-dir` skill)
- **opus の敵対的レビュー (1 周目)**: 書き直しの途中の版が、前の版の deny を allow にする形を 8 つ再現した。根は 3 つで、どれも直した
  - `${..}` を特別に扱う作り (中の `$(..)`・引用符の `}`・`\{` を bash と同じに追えない) → `${..}` を普通の字として読む (`(` `)` で割れる)
  - heredoc の本文を 1 行ずつ独立に割る作り (行継続が切れる。算術の `$((1<<2))`・here-string の `<<<` を heredoc と読んだ後も同じ)
    → 本文全体を 1 つのコマンド列として割る。区切りが見つからない `<<` の後ろは必ず割り直す。`<<<` の中の `<<` は数えない
  - 長い語の照合を飛ばす・割り直しを数だけで数える → basename は `match` で取り (2 乗にならない)、割り直しの量 (512KB) にも上限を置く
  - あわせて前の版からの穴も塞いだ: `bash -c --`・`$'..'` の `\x` と 8 進・`$".."`・-L / -S の値の glob・pkill の後ろの tmux を含む語・
    -L ごとの `$(printf | tr)` の fork (4000 個で 10 秒を越えた。default の照合だけ nocasematch にする)
  - 直さずに hook の冒頭の限界へ書いたもの: `trap '..'`・`python3 -c`・`csh -c`・`xargs tmux`・brace 展開・zsh の `=tmux`・`${x:-tmux}`・
    入れ子の引用符 (`"$(echo "x"; …)"`)・バッククォートの中の ` #`・`pkill tmu`。受ける偽陽性: `rg -e 'tmux' -e 'kill-server'`
  - テストの穴 (OVERFLOW を通す・awk の失敗を通す・大きさの上限を外す変異が緑) → それぞれのケースを足した
- 変異 (最後の版で 25 本): 24 本が red。緑の 1 本は `<<<` の除外を外す変異で、区切りが見つからない `<<` の残りを割り直す仕組みが同じ入力を拾うので判定が変わらない。
  前の周に当てた bash の `${t##*/}` を戻す変異は、大きさの上限 (128KB) の内では 1 語で 8 秒弱にしかならず赤にできないので、テストでは固定していない
- 印の lib: 20 並行 × 30 回の put・詰め直しが何度も起きる並行の書き込みで、行は壊れなかった (レビューの実測)。変異 3 本 (put を何もしない・言語ごとの種類を 1 つにする・__pycache__ を作る) が red
- **opus の敵対的レビュー (2 周目)**: 直した版で、素通りに戻る回帰を 3 系統再現した。直した
  - heredoc の本文を「シェルへ渡すか」で見る・見ないを分けた判定が、パイプの先 (`cat <<'EOF' | bash`)・`. /dev/stdin`・プロセス置換・fd の経由・
    算術の `<<` の後ろの区切りの語で騙された → 見分けるのをやめ、本文は常に 1 つのコマンド列として割る (渡し方が多すぎて静的には閉じない)。
    commit message の本文の偽陽性は前の版と同じに戻した (rule の「強制手段」の回避 = ファイル経由で渡す、はそのまま)
  - シェルの名前の語ごとに `-c` を末尾まで探し直し、`sh` を 16000 並べた入力で 10 秒を越えた → 探し終えた位置から続ける
  - `$'..'` の `\u` / `\U` を解かない・割り直しの絞り込みが解く前の字面に当たる → 解く。`$'` を含む文字列は絞らずに割り直す
  - まとめたフラグの -L / -S (`-uL default`) を見ない → getopt と同じに読む
  - 前の版から allow のまま hook の冒頭の限界に足したもの: tmux 自身が別のシェルで走らせる文字列 (`tmux -c`・`run-shell`・`display-popup -E`・
    `new-window '..'`・`send-keys`)・`kill $(pgrep tmux)`・`osascript -e`・tcsh / fish
- **opus の敵対的レビュー (3 周目)**: 直しのうち 3 つに壊れ方があった。直した
  - 算術の `<<` を heredoc と読むと、区切りの語の行を捨て、引用符の状態が bash とずれて後ろの kill を引用符の中と読む
    → `<<` を見た文字列は「heredoc として読む」と「読まない」の両方で割る (`<<` が heredoc かは静的に決まらない)
  - -S の付属値に c / f / T と大文字の L / S を含むと値を見ない・まとめたフラグの末尾が値を取るフラグ (`-uf /dev/null`) だと次の語を値として飛ばさない
    → 先頭が -L / -S の語は従来の枝で受け、それ以外のまとめたフラグは getopt と同じに 1 字ずつ読む
  - `$'\t'` を多く含む入力が、kill の語が無いのに割り直しの数の上限で deny になる → 絞らずに積むのは字を隠せる escape (`\x`・`\u`・`\U`・8 進) を含むときだけ
  - -c の無いシェルの語の後ろの eval を見ない → -c が見つからなければ以後は探さず、eval は見続ける
  - 性能: 128KB 以下の 18 種で最大 1.5 秒 (負荷の平均 22〜45 の下)。`-uLx` を 2.6 万並べた入力は単独で 1.1 秒
- 変異 (4 周目の直し): 7 本が red。うち `<<` を 2 度目にも heredoc と読む変異は、積み直しが上限に達して OVERFLOW の deny になり、
  「heredoc の本文の -L 付きの kill は通す」の allow が落ちる形で red (狙った算術の << のケースではない)
