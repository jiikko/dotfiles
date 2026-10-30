# 774 (docs): `tmp/` 直下にセッションの検証・git 操作ログが溜まる — 置き場の規約に「名前」と「消す人」が無い

起票日: 2026-10-15

> 🚨 **`waiting/` (2026-10-16〜)**: 残りは「新しい規約で `tmp/` 直下への書き込みが止まったか」の観測だけ。
> **待っているもの**: 規約の入った 2026-10-15 23:04 より**後に始まった** Claude Code のセッションが、2 週間ほど dotfiles で作業すること (目安 2026-10-30)。
> 規約は `~/.claude/CLAUDE.md` としてセッション開始時に読まれるので、それより前から生きているセッションの書き込みは判定に数えない。
> **測り方**: `find tmp -maxdepth 1 -type f -newermt '2026-10-16 09:00' -exec ls -lT {} +` で直下に増えたファイルを出し、書いたセッションの開始時刻
> (`ListAgents` の started) と突き合わせる。`tmp/<skill名>/.skill-tmpdir` の有無で skill-tmpdir が使われたかも見る。
> **判定**: 新しいセッションが直下に書いていなければ、進捗の最後の項目を閉じて `scripts/issue_done.sh 774`。書いていれば、どの手順・どの規約の文面に
> 従った結果かを特定してから、改善案 B (Stop hook で知らせる・clean-tmp で直下のログの寿命を短くする) を検討する

## 概要

`~/dotfiles/tmp/` の直下に、Claude のセッションが書いた使い捨てのログが 1170 ファイル (136 MiB) 溜まっている。
中身は push / pull / rebase / lint / `make test` の出力をリダイレクトしたもので、**どの issue / docs からも参照されていない**。
原因は特定の skill ではなく、規約が「出力はファイルへ落とせ」「置き場は `./tmp`」までしか言わず、
**ファイル名の付け方 (どの階層に置くか) と、誰がいつ消すか**を決めていないこと。

## 実測 (2026-10-15)

- 直下のファイル 1170 件 / 136.44 MiB。ディレクトリ 51 件は別 (mutation の worktree・サンプル等。こちらは本 issue の対象外)
- 更新月はすべて 2026-10 (`find tmp -maxdepth 1 -type f -exec stat -f '%Sm' -t '%Y-%m' {} + | sort | uniq -c`)
- 拡張子: `.log` 979 / `.txt` 112 / `.out` 21 / `.sh` 11 / その他
- 名前の形はほぼ全部 `<issue番号か略称>-<工程><連番>.log` (`548-push.log` / `670-pull3.log` / `zk-push10.log` / `638-maketest2.log`)。
  先頭の prefix は約 360 種類 (= 書いたセッション・作業の数の目安。数え方で 359〜375 に振れる:
  `find tmp -maxdepth 1 -type f | sed 's|tmp/||' | grep -E '\.(log|txt|out|err)$' | sed -E 's/-.*//' | sort -u | wc -l` → 359)
- 工程の内訳 (名前による粗い分類。正規表現次第で ±10% 動く): git 操作 (`push` `pull` `rb` `rebase` `claim` `done` `mv` `links` `sync`) 450 /
  検証 (`lint` `test` `maketest` `mut` `ci` `bench` 等) 264 / その他 456
- 参照: 直下の全ファイル名で `grep -rlF "tmp/<名前>" issues docs _claude` → **0 件**

### zundamon-kaisetsu の残骸か?

**skill の出力ではない**。`_claude/skills/zundamon-kaisetsu/SKILL.md` の手順 3 は作業ディレクトリを
`./tmp/<名前>/` (サブディレクトリ) に取る設計で、その出力は `tmp/it-kaisetsu/` のようにディレクトリへ入っている。
直下の `zk-*.log` / `ci-zk-*.log` は **zundamon-kaisetsu (の Go 実装) を開発したセッションの** push / pull / lint /
`make test` / bench のログで、他の issue の作業 (`548-*` / `670-*` …) と同じ形をしている。

## 原因

1. **「出力はファイルへ落とす」規約が出力先の名前を決めていない**。
   - `_claude/rules/verify-execution-not-just-exit-code.md`: 「検証コマンドの出力を `tail` / `head` / `grep` で削って読まない。ファイルへ落としてから読む」
     「前段は `> log 2>&1; rc=$?` で取り」
   - `.claude/rules/worktree-per-session.md`: 「push の rc は `> log 2>&1; rc=$?` で直接取る」
   - これを守ると 1 回の作業で push / pull / lint / test ごとに 1 ファイルずつ生まれ、再試行のたびに連番が増える (`670-push3.log`)
2. **置き場の規約が直下を既定にしている**。`_claude/CLAUDE.md`「一時ファイルの配置」は「成果物 (レポート・スクラッチ・中間生成物) は `./tmp`」と言い
   (scratchpad は「例外: そのまま使ってよい」と許すだけで、推奨していない)、
   さらに「後で消す worktree の中で走らせる出力は、**本体 checkout の `tmp/` を絶対パスで書く**か scratchpad に置く」と言う。
   dotfiles はセッションごとに worktree を切る (`.claude/rules/worktree-per-session.md`) ので、**全セッションのログが `~/dotfiles/tmp/` 直下の 1 か所に集まる**
3. **消す責任がどこにも無い**。同じ節は「線引きは置き場所ではなく終了時に消す責任が実装されているか」と書くが、
   それは「スクリプト / テストの隔離ディレクトリ」側の話で、セッションが手で書いたログには誰も消す責任を持たない。
   `make clean-tmp` (`scripts/clean_tmp.sh`) は手動起動で、既定は 30 日より古いものだけ — 今ある 1170 件は全部 30 日以内なので対象外
4. ログの用途と寿命が合っていない。push / pull / lint のログは**同じ手順の中で書いて rc と中身を読んだら役目を終える** (`_claude/rules/move-report-conclusions-to-issues.md` の表の 1 行目
   「同じ手順の中で書いて読む出力先」)。セッションを越えて残す理由が無いのに、セッションを越えて残る場所 (gitignore された repo 内) に置いている

対比: `scripts/issue_claim.sh` は push の出力を `"$work/push.log"` (自分で作った一時ディレクトリ) に書き、終わりに消している。道具は正しく、手で打つ作法だけが直下に書いている。

## 改善案

### A. 規約: 使い捨てのログは scratchpad (なければ `mktemp -d`) へ。`./tmp` は「セッションを越えて読むもの」だけ (推奨)

`_claude/CLAUDE.md`「一時ファイルの配置」へ追記する (新規 rule は立てない — 発動点が既存の節と同じ「ファイルをどこに書くか」なので)。案:

- **同じ手順の中で書いて読み終えるログ (push / pull / rebase / lint / test の出力) は、ハーネスの scratchpad に置く**。
  scratchpad が無い環境 (別ハーネス・作業者に scratchpad が見えない) では `tmp/<issue番号か作業名>/` に入れる
  (`mktemp -d` は採らない: macOS では `/var/folders` 配下で OS が消すが、消す責任を実装していない点は今の直下と同じ)
- **`./tmp` に置くのは、セッションを越えて人や次のセッションが読むもの** (レポート・サンプル・成果物) と、
  **後から検証する run の出力** (codex / background / detach した長い run の stdout・rc。`_claude/skills/codex-drive/SKILL.md` の
  「run 固有の永続パス」= scratchpad はセッション終了で消え、空振りか遅れて完了かを後から判定できない。obaket 581) だけ。
  置くなら直下に散らさず `tmp/<issue番号か作業名>/` のディレクトリに入れる (`make clean-tmp` がディレクトリ単位で寿命を判定できる)
- worktree の中で走らせる出力の「本体 checkout の `tmp/` を絶対パスで書く」は、上の「`./tmp` に置くもの」に限る旨に直す
- 併せて `verify-execution-not-just-exit-code.md` と `worktree-per-session.md` の `> log 2>&1` の例を、置き場の正本 (`_claude/CLAUDE.md`) を指す形にする
  (例の中にパスを書き足さない — 置き場の規範を 3 か所へ写すと乖離する)

効果: 新しい直下のファイルは「規約に反して書かれたもの」だけになる。scratchpad はセッション専用なので並行セッションの名前の衝突 (`push.log` の上書き) も起きない。
弱点: 規約は破られる。破られても 1 ファイルずつしか増えず、下の B で拾える。

### B. 機械: 直下に増えたファイルを知らせる (A の後に、必要なら)

- 案 B1: Stop hook で「このセッション中に `tmp/` 直下へ増えたファイル」を数え、あれば注入する (消すかディレクトリへ移すかをその場で決めさせる)。
  セッション開始時の直下の一覧を控える必要があり、並行セッションの分と区別できない (自分が書いたと断定できない) のが難点
- 案 B2: `make clean-tmp` に「直下の `*.log` は 3 日」のような短い寿命を足す。自動では走らないので、効くのは人が叩いたときだけ
- どちらも「作ってから掃除する」形 (`adversarial-review-own-safeguards.md` §0-A が先に問えという形) なので、**A で発生源を絞ってからまだ溜まるかを見て決める**

### C. 今ある 1170 件

参照 0 件なので消してよい (ユーザーの判断。`make clean-tmp DAYS=0 DRY_RUN=1` で一覧を見てから)。
🚨 `DAYS=0` はディレクトリ (mutation の worktree 等) も巻き込むので、直下のファイルだけを消すなら
`find tmp -maxdepth 1 -type f \( -name '*.log' -o -name '*.txt' -o -name '*.out' -o -name '*.err' \) -print` で一覧を確かめてから `-delete` に替える。

## 関連ファイル

- `_claude/CLAUDE.md` の「一時ファイルの配置」
- `_claude/skills/codex-drive/SKILL.md` (run 固有の永続パス。A の例外の出典)
- `_claude/rules/move-report-conclusions-to-issues.md` (「同じ手順の中で書いて読む出力先」)
- `_claude/rules/verify-execution-not-just-exit-code.md` / `.claude/rules/worktree-per-session.md` (`> log 2>&1` の例)
- `scripts/clean_tmp.sh` / `Makefile` の `clean-tmp`
- `_claude/skills/zundamon-kaisetsu/SKILL.md` (手順 3。サブディレクトリに出す正しい形の例)

## 進捗

- [x] 原因の特定と実測 (本 issue)
- [x] 反証レビュー (sonnet 1 体、read-only): 件数・サイズ・参照 0 件・規約の引用・zundamon の判定は反証されず。
  採った指摘: A が codex-drive の永続パス規約と衝突 → 例外を追記 / `mktemp -d` 案は消す責任を再生産 → 削除 /
  CLAUDE.md に scratchpad の例外が既にある → 原因 2 に追記 / ディレクトリ数 50→51 / prefix・内訳の数え方の幅。
  記録のみ: `_claude/agents/test-runner.md` と `skills/perf-analysis` も `./tmp/` 直下 (`thumbnailthumb-test.log` / `perf.log`) を指すが、
  ThumbnailThumb 専用で 1 ファイル固定名 (上書き) なので溜まらない。A の追記と同時に揃えるかは A の実装時に決める
- [x] A の規約追記 (ユーザー承認 2026-10-15): `_claude/CLAUDE.md`「一時ファイルの配置」に、その場で読み終えるログは scratchpad /
  残すものと後から確かめる run の出力は `tmp/<作業名>/` / 直下に散らさない、を追記。worktree の項も `tmp/<作業名>/` に合わせた。
  `verify-execution-not-just-exit-code.md` と `worktree-per-session.md` の例は `> log` とだけ書きパスを持たないので変えていない
  (置き場の正本は `_claude/CLAUDE.md` の 1 か所)
- [x] C の既存ファイルの削除 (ユーザー承認 2026-10-15): 直前に参照を数え直して 0 件 (1170 個の名前を `grep -rnoF -f` で一括照合。
  必ず当たる `tmp/skill-eval/` で較正して 1 件ヒット)。並行セッションが使用中かもしれない直近 1 時間のファイルは除いて
  `find tmp -maxdepth 1 -type f -mmin +60 -delete` → 1170 → 20 件 (残りは issue 747 の作業中のログ)。ディレクトリ 51 件は触っていない
- [x] 敵対的レビュー (opus 2 周、read-only。ユーザー依頼 2026-10-15) を受けて規約を直した:
  1 周目 P2 4 件 — 「スクラッチは ./tmp」と「scratchpad はログだけ」の矛盾 / scratchpad はサブエージェント・fork と共有なのに
  「名前がぶつからない」と書いた / `tmp/<作業名>/` の使い回しは `scripts/clean_tmp.sh` が dir ごと残す / codex-drive を根拠に引いたが skill 内で割れている。
  P3: 「scratchpad はセッション終了で消える」は誤り (実測: 過去 session の dir が 20 個残る。消えるのは再起動)。→ scratch の既定を scratchpad に一本化、
  名前を一意に、`tmp/<作業名>-<日付>/`、move-report の表の例を直下でない形に。
  2 周目 — background / detach の run 出力を永続パスに置く保証が消えていた (P2) → 例外として戻した / headless 向けの行の日付漏れ (P3) → 直した。
  壊せなかった観点: compaction・Stop hook 後の読み戻し、worktree 削除後のログ、codex は CLAUDE.md を読まない
- [x] **直下へ書く skill を `<本体 checkout>/tmp/<skill名>/<session-id>/` へ寄せた** (置き場の形はユーザー指定 2026-10-15)。
  `bin/skill-tmpdir <名前>` が dir を作って絶対パスを返す (session-id は `CLAUDE_CODE_SESSION_ID`。worktree の中からでも本体 checkout を返す)。
  直した skill: codex-review / codex-lead (`ls -t` の復元手順も) / codex-drive / review-loop / zundamon-kaisetsu / perf-analysis (プレビュー png だけ。
  `./tmp/perf.log` はアプリが書く固定パスなので動かせない)。入口は `_claude/CLAUDE.md`「一時ファイルの配置」。
  テスト `tests/bin/test_skill_tmpdir.sh` (20 件)。変異: worktree の分岐を消す → 2 件 red / 名前の検査を外す → 4 件 red /
  submodule の core.worktree の分岐を外す → 1 件 red / GIT_DIR の unset を外す → 1 件 red / 本体での警告抑止を外す → 1 件 red
  - 敵対的レビュー (opus 1 本、read-only) の指摘: P2 submodule・--separate-git-dir の worktree では common-dir の末尾が .git でなく、
    worktree の中に tmp/ を作っていた → core.worktree で本体を引く / 本体の場所を git が持たない separate-git-dir の worktree は
    cwd の checkout に倒して警告する。P3 継承した GIT_DIR に引きずられる → unset / codex-review の削除規約に旧ファイル名 /
    zundamon-kaisetsu に「1 回打ってリテラルで使う」が無い → 直した。P3 bare repo では git dir の中に tmp/ を作る → 旧 `./tmp` と同じなので据え置き。
    壊せなかった観点: 通常の worktree・symlink 経由・名前と sid の検証・session 共有での誤削除 (rm は完全一致パスのみ)
  - skill-eval: `PASS` codex-drive / codex-lead / codex-review / perf-analysis / review-loop (各 1/1)・zundamon-kaisetsu (2/2)、計 約 $2.2
  - `make test`: test-lint は通過。test-src の Go lint (src/pro-con の flockmark_test.go の errcheck・別 package の display.go の gofmt) が赤。
    この差分は Go に触れていないので、origin/master に先に入った他の作業の赤
- [x] **codex-drive の置き場の割れを解消**: [2] の `./tmp/<タスク>/`・[3.5]/[3.6]/[3.7] の `<scratchpad>/…`・設計ファイル `./tmp/codex-drive-design.*`
  を「出力の置き場 (`<D>`)」節の 1 か所 (`skill-tmpdir codex-drive`) に寄せ、「scratchpad はセッション終了で消える」の誤りを直した
- [x] skill-eval: eval の無かった 5 本 (codex-drive / codex-lead / codex-review / perf-analysis / review-loop) に「発火するか」のケースを足した
- [x] **`make clean-tmp` が `tmp/<skill名>/` の中の古い session の dir を消す** (ユーザー承認 2026-10-16)。`skill-tmpdir` が `tmp/<skill名>/.skill-tmpdir` に
  印を置き、`scripts/clean_tmp.sh` は印のある dir を丸ごと判定せず、中の session の dir を 1 件ずつ既存と同じ judge で判定する。
  敵対的レビュー (opus 1 本) の指摘を直した: P1 clone・worktree の dir に印があると兄弟の `.git` や `src/` を消した → 直下に git の印があれば降りない /
  P1 一覧から削除までの間に skill の dir を symlink に差し替えると外を消した → 消す直前に親が実体の skill の dir かを取り直す /
  P2 消す直前に自身の mtime を見ない (作ったばかりの空の session の dir を消しうる) → 取り直す / P3 印の大文字小文字・読めない skill の dir で全体が止まる・
  先の無い symlink の印を辿って外に作る → 直した。テスト `tests/scripts/test_clean_tmp.sh` の 11〜11i。
  変異: 直下の読み飛ばし・skill の中のループ・印の symlink 検査・skill の dir の symlink 検査・git の印の確認・親の取り直し・mtime の取り直し
  (一覧の絞り込みと 2 段なので両方外して red)・-iname・skill-tmpdir の `-L` → いずれも red。読めない skill の dir の分岐は手前の `is_skill_dir` の find の失敗で
  丸ごと判定に回るので、このテストの形では到達しない (11h はその経路で振る舞いを見る)
  敵対的レビュー 2 周目 (opus): P2 親の取り直しと rm の間に差し替えると外を消せた → 削除はすべて「サブシェルで親へ cd → pwd -P が実体と一致 →
  `./名前` を消す」に (直下のエントリも同じ形。tmp 自身の差し替えも防ぐ) / P2 祖先の差し替えと mtime の取り直しをテストが見ていない →
  11j (rm の shim で直前に差し替え)・11k (du の shim で tmp を差し替え)・11l (一覧の後に更新) を足した / P3 印が途中で消えると二重に数える →
  skill の dir を 1 回だけ決めて両ループで使う。変異: pwd -P の確認を外す → 11k red / mtime の取り直しを外す → 11l red。
  絶対パスの rm に戻す変異は Claude Code の安全チェック (rm -rf を含む文字列) に止められて当てていない (その形が外を消すことはレビュアーが shim で再現済み)
  3 周目 (opus): P2 builtin の `pwd -P` は cwd を見ず `$PWD` の文字列を解決し直すだけで、cd の後に親を実体へ戻されると外の cwd を一致と答える
  (手元で再現: symlink の d へ cd → d を実体に戻す → builtin は .../d、`/bin/pwd -P` は .../other) → 比較と基準の root / tmp を `/bin/pwd -P` に揃えた。
  cd と比較の間は外部の呼び出しが挟まらず shim で窓を作れないので、テスト 12 で形を固定した (builtin に戻す変異で red)。
  P3 (旧実装から同じ・据え置き): 1 件の rm の失敗で exit 1 し、残りの判定と集計が出ない。壊せなかった観点: 名前の細工 (先頭の -・改行)・
  dirp の綴り・rc の取り違え・in_skilldirs・テストの shim の窓
  4 周目 (opus): P2 (変更前から在る) 継承した GIT_DIR / GIT_WORK_TREE で root が repo の外を向き、外の tmp を消す対象にした → 冒頭で unset (テスト 13。
  unset を外す変異で red) / P3 テスト 12 は件数の下限が砦で、別の箇所に /bin/pwd -P を足すと論理 pwd の比較が通る → 出現を数え、削除の直前の比較の形そのものを固定
  (論理 pwd + 余分な /bin/pwd の変異で 12 と 11k が red) / P3 cd の失敗も「実体でなくなった」と表示 → 文言を直した。
  壊せなかった観点: 大文字小文字・symlink・firmlink・NFD/NFC の経路での綴り、/bin/pwd の差し替え、getcwd の失敗 (残す側に倒れる)
  5 周目 (opus。角度を変えた: 変更前との全体比較・性能・skill-tmpdir との競合・記録の正しさ): P2 同じ session ID で DAYS 日後に skill-tmpdir を呼ぶと
  (再開)、`mkdir -p` は既にある dir の mtime を更新しないので、返した直後のその dir を clean が消した (旧版は兄弟が新しければ dir ごと残していたので悪化) →
  skill-tmpdir で `touch "$dir"` (テスト 1b。外す変異で red) / P3 ヘッダの「消す直前に mtime を取り直す」は言い過ぎ (取り直しは判定の始め) → 文言を直した。
  性能 (DRY_RUN、レビュアーの実測): 古いファイル 1000 件 旧 2s → 新 6s / dir 2000 + skill の dir 101 + session 2000 旧 20.8s → 新 62.9s /
  候補 1000 × skill の dir 1000 旧 13s → 新 26s。実際の tmp は数十〜百件で 1 秒以下なので据え置き (trigger: tmp が数千件で clean-tmp が遅いと感じたら
  in_skilldirs の線形探索と judge 冒頭の find を見直す)。変更前との比較: 直下のエントリだけの tmp では出力・rc・消すものが skill の dir の行以外一致
  6 周目 (opus): P2 `touch` は dir が消えていると空のファイルを作り、その session の skill-tmpdir が以後 File exists で失敗し続ける (順序を手で並べて再現。
  mkdir と touch の間に clean が入る競合そのものは未発火) → `touch -c` の後にもう一度 `mkdir -p` / P3 touch の失敗 (所有者の違う dir) で止まる → 警告して続行。
  壊せなかった観点: touch と judge の新しい更新の判定の噛み合い、印の作成のずれ。残る窓: judge の判定の始めから rm までの間に touch が来ると、消された dir の
  パスを返す (旧実装と同じ種類で、ミリ秒の見積もり・未計測)。敵対的レビューはこの周で閉じた (5h 枠 87%)
- [x] **ThumbnailThumb の agent** (ユーザー指示 2026-10-16): smoke-test-runner / tt-api-expert の `tt-client -o` の出力先を `skill-tmpdir` に寄せた
  (`tt-client` の preview / export はクライアント側の curl が保存して親を mkdir する: `~/src/homebrew-tap/bin/tt-client`)。
  test-runner / swiftui-test-expert の `./tmp/thumbnailthumb-test.log` 等は ThumbnailThumb の `make test-debug` が書く固定パスを読むだけなので変えない。
  smoke-test-runner が詳細を任せる ThumbnailThumb の `.claude/commands/smoke-test.md` (別 repo) は未確認
- [x] `tmp/` のディレクトリの片付け (ユーザー承認 2026-10-16): 44 件を削除。残したのは issue から参照される `747-mock` / `747-mock2` / `treefiler-mock` / `review-bench`、
  使用中の `747-mdns`、zundamon の完成物 `it-kaisetsu` (2026-10-17 にユーザー承認で削除。動画はアップロード済み)。直下のファイルも 1 時間より古い 20 件を追加で削除 (残り 12 件は 747 などの作業中のログ)
- [ ] A の後 2 週間ほどで直下の増え方を測り、B が要るかを決める (waiting。冒頭の注記)
  - 途中経過 (2026-10-16 08:50): 規約の後に直下へ増えたファイルは 23 件 (`747-*`・`bonjour-*`・`flaky-*`・`dump-check.*`)。生きているセッションは
    どれも 11 時間以上前 (規約より前) に始まっていて、新しい規約を読んだセッションの書き込みはまだ 0 件観測できていない (判定に使えない)

- 2026-10-18: 747 は done。`tmp/747-mock` / `747-mock2` / `747-mdns` を残す理由 (747 の作業中) は無くなったが、747 の本文から参照されているかは片付けの前に確かめる (継続)
