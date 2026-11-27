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

A. **commit の後に、その commit で足した関数の候補を出す** (止めない。PostToolUse の additionalContext だけ。push の前に知らせる)
   - PostToolUse / PostToolUseFailure (Bash) の新しい hook。`lib/git_cmd_detect.sh` の `git_cmd_invokes "$cmd" commit` が真のときだけ走る。
     見る repo は cwd と、`cd` / `-C` の行き先 (どれも候補で、下の「今の commit か」で絞る)
   - **読むのは HEAD の commit の差分** (親が 1 つの commit。親の木と HEAD の木の `.go` / `.swift`)。commit の種類 (stage・pathspec・`-a`・`-i`・`-p`) ごとに
     対象を決め直さない。heredoc の本文・他人の WIP・`git add … && git commit` の時点のずれは、実際に入った中身だけを読むので関係しない
   - **今の commit か**: reflog の先頭が commit (`commit` / `commit (amend)` / `commit (initial)`) で、その記録が 2 分以内のときだけ見る。同じ commit は 1 回だけ見る (印 `commit <sha>`)。
     heredoc の本文の `git -C /other commit` が行き先を足しても、その repo の reflog が新しくなければ何もしない
   - **足した定義そのものを候補から除く**。commit の後は dupscan index が足した定義を読むので、除かないと自分が最初に当たり、短い既存の名前を引く段に進まない。
     除くのは打ち切り・上限の判定の前
   - 判定の本体 (新しい関数の見つけ方・名前の照合・1 セッション × 名前で 1 回の印・計測のログ) は、今の hook の python を `_claude/hooks/lib/dupscan_inject.py` に移して共有する。
     写すと照合が 2 つになり、片方だけ直す形になる。印 (`name <名前>`) を共有するので、Edit の時点で引いた名前は commit で繰り返さない
     (Edit で候補が 0 件だった名前も黙る。同じ索引を引くので結果も同じ。別の repo の同じ名前も黙るのは受け入れる)
   - 移すときに今の hook の挙動を変えない: `python3 -I -B` で起こし、lib の dir を `sys.path` に明示して足す。lib の読み込み・構文の失敗も rc=0 で黙る (shell の側で吸収する)。
     時間の上限は処理の始まりから数え (今は宣言の抽出・git の後から数えている)、型の `git grep` の時間切れで、それまでに拾った候補を捨てない
   - 計測のログ (`claude-dupscan-inject.log`) に入口 (edit / commit) の列を足す。`scripts/dupscan_usage_report.py` は 4・5・6 列の行を読み、commit の行を別に数える。
     数えるのは引いた回数と候補ありの回数までで、その後の探索との結びつきは測らない
C. **共通の CLAUDE.md の「Bash (python / sed) でファイルを編集するなら」の項に、Write / Edit にだけ掛かる hook (`inject-dupscan-candidates-on-new-func.sh` の候補) も通らないことを足す**。
   関数・型を足す編集は Edit / Write で行う (A は commit の後でしか知らせないので、書く前に見る経路を優先する)

見送り: B (名前の照合を「後ろに含む」まで広げる)。候補が増えて読み流されるかを測ってから決める。A の計測のログで、commit の時点の候補の数を見てから再提起する。
B を入れても、名前の違う同じ役割 (`typedPath` と `TypedPathResolver.resolve`) は拾えない。それは定期の監査 (`/audit` の duplication・entry-parity) の担当

先行の判断との関係: [902](epic/890/pending/902-feat-dupscan-claude-code-hook.md) (書いた直後に本体の形で照合する hook) はユーザーの決定 (2026-10-24) で保留し、
形の写しを止める網は `make test` と CI の gate に置いた。A は形の照合ではなく、既にある名前の候補 (948 A1) を出す時点を 1 つ増やすもので、902 の保留を解かない
([1009](epic/932/pending/1009-design-dupscan-post-write-body-similarity-candidates.md) も同じ)

### 反証レビュー (codex, 2026-11-27) で直したこと

- P1: 最初の案は commit の前 (PreToolUse) に対象と中身を決めていた。stage した後に作業ツリーを変えた commit・`git add … && git commit` の前の時点・他人の WIP で
  違うものを読む → commit の後に HEAD の差分を読む形に変えた
- P1: 足した関数自身が「既存の定義」として先頭に当たり、短い既存の名前を引く段に進まない → 除いてから打ち切る
- P2: heredoc の本文の `git -C … commit`・`cd` → reflog の新しさで絞る。時間の上限が hook 全体を数えていない・型の検索の時間切れで候補を捨てる・lib に移すと fail-open が崩れる・
  report が 6 列の行を落とす → 上の方針に入れた。C は hook を名指しする (Bash にも掛かる Write / Edit の hook があるため)

## 関連ファイル

- `_claude/hooks/inject-dupscan-candidates-on-new-func.sh`・`_claude/hooks/lib/git_cmd_detect.sh`・`_claude/hooks/git-state-verify.sh` (cwd と `-C` の扱いの手本)
- `_claude/settings.json` (PreToolUse の Bash)
- `tests/claude/test_inject_dupscan_candidates_on_new_func.sh`
- `scripts/dupscan_usage_report.py`・`docs/dupscan-overview.md`・`_claude/skills/dupscan/SKILL.md`・`src/dupscan/README.md` (hook を説明している入口)
- `_claude/CLAUDE.md` (C)

## 進捗

- [x] 判定の本体を lib へ移す (今の hook のテストが緑のまま): feat(hooks): commit の後に、その commit で足した関数の dupscan の候補を出す
- [x] commit の時点の hook とテスト (変異で赤を見る): 同上。テスト 30 件、変異 17 本で狙ったテストだけが赤
- [x] 計測のログの入口の列と report: 同上
- [x] C: CLAUDE.md: 同上
- [x] 入口の文書 (docs/dupscan-overview.md・skill dupscan・src/dupscan/README.md): 同上 (skill-eval dupscan PASS)
- [x] push して本体を pull し、新しいセッションで注入を観測する (2026-11-27): `~/dotfiles` を origin に追いつかせた後、一時の repo で
  `claude -p --model haiku` に Bash で関数を足して commit させた。transcript の attachment に PostToolUse:Bash の注入
  (「`collectWidgetsFast` に近い既存の定義: src/a/c.go:3 collectWidgets」。足した定義そのものは出ない) があり、hook は 172 ms で終わった。
  同じセッションに C の CLAUDE.md の文も読み込まれていた

### 敵対的レビュー (codex, 3 周) の結果

- 1 周目で直したもの: commit を含まない巨大なコマンドで判定の走査に 17 秒 (「commit」を含まないものは走査の前に抜ける) / cwd を無条件に見て、別の repo への
  `git -C` の commit で cwd 側の新しい commit を出す (commit の行き先だけを見る) / 移動を新しいファイルと読む (-M) / 未来の時刻の reflog を通す /
  HOME 未設定で rc=1 / テストの穴 (既存ファイルへの追記・stage 後の作業ツリーの変更・偽の dupscan が引数を見ない・settings.json の配線)
- 2 周目で直したもの: 共有の hook_strip_heredocs がコメントの中の `<<EOF` で本物の commit の行まで落とす (heredoc を落とすのをやめた) /
  長い message の heredoc と多バイトの字で上限を素通り (上限を 20,000 バイトで数える) / 引用符で囲まれた移動元のパス (-z)
- 3 周目で直したもの: heredoc の本文に git commit を並べると行き先の数だけ rev-parse を打つ (行き先の重複除去) / UTF-8 でないパスで全体が黙り、
  CR が LF に変わる (バイトで受けて surrogateescape で読む)
- 受け入れた制限 (hook の冒頭の「検出しない形」に書いた): heredoc の本文の `cd`・`git -C B commit` (B の reflog が 2 分以内のときだけ表に出る。
  直すには heredoc の構文解析が要り、共有の除去の処理は本物の commit を落とす) / 実行されなかった commit (`false && git commit`・`--dry-run`) /
  `cd B | cat; git commit` (検出器の既知の制限) / 類似度 50% 未満の移動 / 20 KB を超えるコマンド / merge・cherry-pick・rebase / HOME が無いときの `~`
- 記録だけにしたもの: 本物の dupscan が本体の checkout で自動ビルドする経路 (Write / Edit の入口も前から同じ) / commit の印 (SHA) は読み直しを省くだけで、
  出力は名前の印で決まる (外す変異はテストで区別できない) / 偽の dupscan は固定の行を返す (候補が実在するかは本物の index に任せる) /
  UTF-8 でないパスのテスト (macOS の APFS がその名前のファイルを作らない)
