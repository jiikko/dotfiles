# 613 (test): shell のテストに残る秒数の待ち (固定待ち 5・窓 8・時刻跨ぎ 3) と、手書きのポーリング 38 本

> 🚨 **担当中: dotfiles-38**（2026-10-02〜）

起票日: 2026-10-02

## 概要

ユーザーの依頼 (2026-10-02): 「テストで sleep している箇所を、fake timer などの仕組みでカバーできないか検討して。sleep で待ち合わせるのは
不安定で遅くなるので禁止にしたい。そのようなコードを issues に書き出して」。

`tests/` 配下の shell / bats / zsh テストの `sleep` を全数分類した (コメント行を除く 112 行。最初の grep `sleep +[0-9.$]` は
`sleep "$x"` の形を落としており、反証レビューの指摘で 5 行を足した。
sonnet の read-only 調査を main が抜き取りで検閲)。2026-09-05 の分類 ([262](pending/262-perf-test-suite-speedup-plan.md) の節 E) 以降に
増えた分と、そこで「触らない」とした分を含む今の全量。Go のテストは [614](614-test-go-tests-wait-real-time.md)、新しい sleep を止める検査は
[615](615-chore-gate-new-sleeps-in-tests.md)。

規範の正本は `_claude/rules/avoid-wall-clock-assertions.md` (待つなら条件、判定は「何が起きたか」)。

## 全数勘定 (112 行)

| 分類 | 件数 | 扱い |
|---|---|---|
| ポーリングの刻み (TICK) | 45 | 規範の例外。ただし **38 本が `tests/lib/wait_until.sh` の `tt_wait_until` を使わず手書き** (下の C) |
| ダミープロセス (DUMMY: kill される前提 / 生かすだけ) | 39 | 実時間を待たない。対象外 |
| 素の固定待ち (FIXED) | 5 | **直す** (下の A) |
| 競合の窓・遅い処理を演じる入力 (WINDOW) | 8 | 同期点 (ゲートファイル) へ置き換えられる (下の B) |
| 起きないことの確認 (NEGATIVE) | 6 | ポーリング不可。理由のコメントが無いものに書き足す (下の D) |
| 時刻の粒度を跨ぐ (MTIME) | 3 | 待たずに時刻を打つ / 閾値を差し替える (下の B) |
| PATH 上の sleep stub・環境変数で有効になる本物の sleep (既定では待たない) | 3 | 対象外 |
| 文字列・メッセージの中の `sleep` | 3 | 対象外 |

検閲で 1 件直した: `tests/tmux/test_schedule_keys.sh:323` は FIXED ではなく NEGATIVE
(「起動 0.5 秒後にまだ送っていない」を見る否定の assert)。上の件数は直した後のもの。

通常経路で実際に待つ秒数 (見積り): FIXED 約 2.3 s / WINDOW 約 7 s (名目。170 行と 98 行の分を足した見積り) / MTIME 0.9 s / NEGATIVE 約 2.5 s。
実測: `test_schedule_keys.sh` 27 s (うち本物の発火待ち 2 s × 3 は仕様の時間) / `test_go_autobuild.sh` 14 s / `test_run_make_targets_parallel.sh` 4 s。

## A. 固定待ちを条件待ちにする (FIXED 5 件)

| 場所 | 今 | 置き換え |
|---|---|---|
| `tests/tmux/test_smooth_scroll.sh:175` | `source-file` の後に `sleep 0.5` | `source-file` は同期なので不要の見込み。要るなら `list-keys` の C-u が 1 本になるのを `tt_wait_until` |
| `tests/tmux/test_schedule_keys.sh:351` | sleeper が眠るのを 0.5 s で待つ | `j9.pid` の出現を `tt_wait_until` |
| `tests/tmux/test_schedule_keys.sh:375` | 1 回目の送信中に割り込む窓を 0.5 s (+ stub の SEND_DELAY 1 s) で当てる | stub が送信開始で marker を置き release ゲートを待つ → テストは marker 待ち → TERM → ゲートを開ける |
| `tests/tmux/test_schedule_keys.sh:384` | 同上 | `j2.pid` の出現待ち → job 削除 |
| `tests/tmux/test_schedule_keys.sh:575` | 取消後に sleeper が死ぬのを 0.3 s で待つ | `kill -0` が失敗するまで `tt_wait_until` |

## B. 窓・時刻跨ぎを同期点にする (WINDOW 8 / MTIME 3)

🚨 窓の `sleep` は**入力**で、テストの主張を決めている。置き換える前に、その sleep が窓のほかに何を担保していたか
(完走順・到達順・プロセスの寿命) を列挙する。262 の E-1 で、完走順を兼ねていた sleep をゲート化して 12 回に 1 回落ちた前例がある。
置き換えたら変異検証をやり直し、並行テストなので 3 回連続 green を見る。

| 場所 | 今 | 置き換え |
|---|---|---|
| `tests/bin/test_mutate_verify.sh:753` | 変異側の `sleep 3` で hang を演じる (bash は trap を子の終了まで遅らせる) | 変異側を「started を置いて gate を待つ」形に。TERM の後にゲートを開ける (ログ出現の同期点は 756 行に既にある) |
| `tests/zshrc/test_dotfiles_check_result_ownership.sh:192` | rm の shim を 0.5 s 遅くして窓を広げる (issue 577) | rm の shim に 2 つ目のゲートを持たせ、`.partial` が在る状態を観測してから開ける |
| `tests/zshrc/tmux-session/test_resurrect_lock_acquire.sh:134` | mkdir と owner 記録の間を 0.4 s で広げる | 「入った」marker + release ファイル (144 行の marker 待ちと同じ形) |
| `tests/scripts/test_run_make_targets_parallel.sh:30,34` | 完了順を 1 s / 0.3 s で作る | 前の完了をゲートファイルで待たせて順を決める |
| `tests/zshrc/tmux-session/test_resurrect_lock_acquire.sh:170` | `sleep "$1"` で 2 本の worker の起動をずらす | 1 本目が「入った」marker を置くまで待ってから 2 本目を起こす (ずらす幅そのものが主張なら残す。着手時に確かめる) |
| `tests/scripts/test_golangci_lint_pinned.sh:40` | 偽の `go install` を `FAKE_SLEEP=0.3` で遅らせ、並行の install を重ねる | install の開始を marker で知らせ、ゲートで止める |
| `tests/tmux/test_schedule_keys.sh:98` | tmux の stub の送信を `STUB_SEND_DELAY` (1 s) で遅らせる | A の 375 行と一緒に、送信開始の marker + release ゲートへ |
| `tests/tmux/test_smooth_scroll.sh:161,180,203` | リピート判定 (150 ms) を跨ぐ `sleep 0.3` | 閾値を環境変数で差し替える、または前回押下の時刻を過去へ打つ (scroll.sh の実装は未読。着手時に確かめる) |

## C. 手書きのポーリングを `tt_wait_until` に寄せる (38 本)

壁時計はほぼ縮まないが、上限の書き方・出力先・時間切れの扱いがファイルごとに割れている
(`tests/lib/wait_until.sh` の冒頭が「コピペするな」と書いている状態と逆行)。多いファイル: `test_smooth_scroll.sh` 6 /
`test_socket_cleanup.sh` 3 / `test_reap_orphan_servers.sh` 3 / `test_run_make_targets_parallel.sh` 2 / `test_resurrect_lock_acquire.sh` 2 /
`tests/zshrc/test_dotfiles_check_result_ownership.sh` 3 / `test_kernel_alloc_watch.sh` 2 / `codex_fanout.bats` 2 (bats は helper を source していない) ほか。
`tests/bin/test_go_autobuild.sh` の `wait_for` と `test_schedule_keys.sh:1131` の `wait_for` は `tt_wait_until` と同形の重複。

## D. 否定の確認 (NEGATIVE 6 件) は残し、理由を書く

| 場所 | 理由のコメント |
|---|---|
| `tests/bin/test_mutate_verify.sh:572` (0.2 s) | あり |
| `tests/bin/test_go_autobuild.sh:347,411,473` (0.5 s × 3) | **無い**。書き足す。代替案: 再挑戦するかは shim が exec 前に同期で決めるので、`GO_AUTOBUILD_PENDING` (spawn したか) を観測すれば待たずに判定できる見込み (未確認) |
| `tests/tmux/test_schedule_keys.sh:323` (0.5 s) | 無い。書き足す (発火時刻より前に送らないことの確認なので、待ち自体は仕様) |
| `tests/tmux/test_schedule_keys.sh:692` (0.3 s) | 無い。書き足す。🚨 待たずに `kill -0` で見る案は**成り立たない**: シグナルの配送は非同期なので、誤って殺す実装でも直後の `kill -0` は通る (反証レビューの指摘) |

## 対象外とした行

- DUMMY 39 件 (`sleep 3600 &` / `sh -c 'sleep 30'` / kill される fake)。実時間を待たない
- `tests/zshrc/lazy-loading/test_version_managers.sh:16`: cleanup の rm リトライの backoff。失敗時しか走らない (262 の E-2 と同じ判断)
- `tests/tmux/test_schedule_keys.sh` の本物の発火待ち (`STUB_REAL_SLEEP=1` の 2 s): 時間そのものが仕様。縮めるなら発火時刻の計算に時計の注入が要る
  (`date +%s` を差し替える口)。今回の調査では見送り

## 受け入れ条件

- [x] A の 5 件を条件待ちにし、変更したテストの実行時間を before / after で記録する
- [x] B を 1 件ずつ、担保していたものの列挙 → 置き換え → 変異検証 → 3 回連続 green (置き換えられないものは理由つきで残した)
- [x] C を `tt_wait_until` へ寄せる (bats は見送り。下の進捗)
- [x] D の理由コメントを書き足す (代替案が成り立つものは置き換える)
- [ ] [615](615-chore-gate-new-sleeps-in-tests.md) の検査の許可 (印 / ファイル単位の許可) を、この表の残り (TICK / DUMMY / NEGATIVE / STUB) と一致させる

## 関連ファイル

- `tests/lib/wait_until.sh` (`tt_wait_until`)
- `_claude/rules/avoid-wall-clock-assertions.md`
- [262](pending/262-perf-test-suite-speedup-plan.md) の節 E (2026-09-05 の分類と、窓のゲート化の前例)

## 進捗

- 2026-10-02 「test(tmux): schedule_keys の固定待ちを「sleep に入った」記録とゲートで待つ (613 の A)」
  - 351 / 384 (現 j9 / canc) と 323 (j2): `/bin/sleep 0.5` → sleep の stub が本物の sleep の前に `$CALLS` へ書く行を待つ (`entered_sleep`)。
    「眠っている間に入れ替える」が眠る前に当たって、眠る前に判定する誤った実装でも緑になる余地が消えた。323 は否定の確認のままだが待ちが事象になった
  - 375: `STUB_SEND_DELAY=1` + `sleep 0.5` → 送信の stub が自分の pid を書いてゲートで止まる → テストが fire と stub の両方へ TERM → ゲートを開ける
  - 🚨 375 は置き換え前から **vacuous** だった: stub は最初の行で呼び出しを記録し、送信は tmux の 1 回の呼び出しなので、`trap '' TERM INT HUP` を
    外す変異でも緑 (置き換え前の版で実測)。本物の機構は「無視したシグナルが子の tmux に引き継がれる」ことなので、stub に TERM を撃ち、
    送り終えた記録 (`send-complete`) で判定する形に直した
  - 575: `sleep 0.3` + `kill -0` → `process_gone` を上限つきで待つ。692 は否定の確認なので待ちを残し、理由をコメントに書いた (D)
  - 手書きの `wait_for` を `tt_wait_until` の薄い包みにし、ファイル冒頭へ移した (C の 1 本)
  - 実測: 26.5 s → 23〜24 s (3 回連続 green、146 件の ✓ は変更前と同数)
  - 変異 (mutate-verify、すべて想定の検査で red): サーバの同一性を眠る前に取った値で判定 → 「眠っている間にサーバが入れ替わったら送らない」/
    `trap '' TERM INT HUP` を外す → 「送信中の tmux が最後まで走る」/ 取消で kill しない → 「取消したのに sleeper が生きている」
- 2026-10-02 「test(tmux): smooth_scroll の押下の間の固定待ちを消す (613 の A / B)」
  - 175 (`source-file` の後の 0.5 s) と 161 / 180 / 203 (リピート判定を跨ぐ 0.3 s) を削除。6 s → 3〜4 s (3 回連続 green)
  - 🚨 跨ぐ待ちは**元から結果に効いていなかった**: 直前の `wait_settled` が 0.1 s 刻みで 3 回の同値を見るので、前回押下から 150 ms は必ず過ぎている。
    残る分岐 (アニメ中の再押下 = `anim_until_ms` の内側で即時ジャンプ) は、assert がどちらの経路でも同じ移動量を求めるので区別されない。
    状態ファイルの `last_press_ms` を flock の下で 0 にする helper も試したが、それを何もしない形にする変異でも緑だったので入れなかった
  - 残る穴 (未着手): 押下 4・5 が「アニメで動いたか / 即時ジャンプか」を見る assert が無い (状態ファイルの 3 列目 `anim_until_ms` が 0 かで読める)
- 2026-10-02 「test(bin): mutate-verify の中断のケースで hang を秒数で演じない (613 の B)」
  - `tests/bin/test_mutate_verify.sh` の 42 番: 変異で差し込む `sleep 3` を「ゲートが開くまで待つ子」に替え、TERM の後にゲートを開ける。
    40 s → 34〜35 s (3 回連続 green、42 ケース)。変異: 中断時の `report_logs` を外す → 「中断されたら全体のログが残らない」で red
- 2026-10-02 「test(tmux): resurrect の lock 奪取の production 形状を、秒数の窓ではなくゲートで開ける (613 の B)」
  - `test_resurrect_lock_acquire.sh` の fresh 形状 (5 回): `tt_proc_starttime` の `sleep 0.4` + ずらし `sleep 0.05` →
    1 本目が窓に入ったら印を置いてゲートで止まる → 2 本目を起こす → 2 本目が結果を書く (正しい実装は即 rc=1 で退く) か同じ窓に入るまで待つ → ゲートを開ける。
    6 s → 5 s (3 回連続 green、✓ 37 件は変更前と同数)
  - 変異: 作りたての owner 不在 dir を奪わないガード (`tt_lock_dir_older_than` の分岐) を外す → 「lock 不在から 2 プロセスが来たとき」で red
  - 残す: leftover / deadowner の 1〜5 ms のずらし (`sleep "$1"`) は臨界帯を掃く入力。ゲートにすると掃く意味が消える
  - 残す: `tests/zshrc/test_dotfiles_check_result_ownership.sh:192` の rm の 0.5 s は、ハーネス自身が「bg の終了を待つ」ことを守る窓
    (固定 sleep で待つハーネスなら必ず落ちる)。ゲートにすると bg が終わらなくなり、守る対象を崩す。1 回 0.5 s
  - 作業中の事故: 最初の版は関数の中で `$5` (関数自身の引数 = 空) を使い、worktree の直下に印のファイルを 15 個作った。
    さらに、その版の 3 回ループのバックグラウンド実行が残って後の計測に混ざった。ループを止め、残骸を消してから測り直した (上の数字は測り直したもの)
- 2026-10-02 「test(scripts): 並列ランナーの完了順を秒数でなくゲートで作る (613 の B)」
  - `test_run_make_targets_parallel.sh` の `slow-ok` の `sleep 1` / `mid-ok` の `sleep 0.3` を外し、完了順が要る Test 4 だけに
    `ord-late` (ord-early の完了印を待つ) / `ord-early` を足した。4 s → 1 s 以下 (3 回連続 green、14 件)
  - 変異: 出力を完了順 (`.rc` の mtime 順) に並べる → 「出力順が引数順でない」で red
- 2026-10-02 「test(bin): go_autobuild の「再挑戦しない」を待たずに、shim が裏ビルドを起こしたかで判定する (613 の D)」
  - 347 (TTL 内) / 411 (失敗後の backoff): `sleep 0.5` → 再挑戦の判定は shim が exec の前に同期で下すので、バイナリに
    `pending=${GO_AUTOBUILD_PENDING-} old` を焼いて起動の出力で読む (最後の語を old に保ち `binary_is` を壊さない)
  - 473 (生きている builder の lock を尊重): 判定は裏で起こした builder の中 (`_go_autobuild_take_lock`) で、取れないと何も書かずに抜ける。
    待つべき成立条件が無いので待ちを残し、理由をコメントに書いた
  - 20 s → 12〜13 s (3 回連続 green、75 件。負荷の揺れを含む)
  - 変異: `_go_autobuild_should_retry` を常に 0 → 「TTL 内なのに再挑戦した (起動の出力: pending=1 old)」で red
  - 残す: `tests/scripts/test_golangci_lint_pinned.sh:40` の `FAKE_SLEEP=0.3` (6 本の並列起動で install を重ねる窓)。ヘルパーは lock で
    直列化するので「残り 5 本が lock を待ち始めた」を外から観測する事象が無い。1 回 0.3 s
- 2026-10-02 「test: 手書きのポーリングを tt_wait_until に寄せる (613 の C)」
  - 16 ファイル (sonnet が書き換え、main が diff を検閲)。上限は元以上 (`TT_WAIT_TICKS` / `TT_WAIT_TICK` を呼び出しに付ける)、
    条件は関数、時間切れの扱い (fail の文言・exit・dump) は元のまま。`tests/tmux/lib/kill_socket.sh` は自分で `wait_until.sh` を読む
    (利用者 4 本はすべて bash)。各ファイル変更前 1 回・変更後 3 回とも変更前と同じ rc
  - 変更したテストのシェルに `sleep` の stub が PATH で入っていないことを確かめた (入っていると `tt_wait_until` が待たずに上限を使い切る)
  - 触らなかった: `tests/zshrc/tmux-session/test_tt.sh:292` (テストが `sleep() { : }` で sleep を潰しており、`command sleep` で回避している) /
    `bash -c '...'` の文字列・heredoc の中のループ / `test_go_autobuild_warmup.sh` の `runs_within` (上限が引数の判定器) /
    `codex_fanout.bats` (bats は helper を source していない)
  - 未検証: `tests/tmux/test_fork_scratch.sh` は macOS では skip (rc=77) なので、置き換えの動作は構文確認 (`zsh -n`) だけ
