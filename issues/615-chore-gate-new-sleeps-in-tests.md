# 615 (chore): テストに新しい sleep を足せないようにする検査 (shell は印つきの許可、Go は forbidigo)

> 🚨 **担当中: dotfiles-38**（2026-10-02〜）

起票日: 2026-10-02

## 概要

ユーザーの依頼 (2026-10-02): テストで sleep による待ち合わせを「禁止にしたい」。[613](613-test-shell-tests-wait-with-sleep.md) (shell) と
[614](614-test-go-tests-wait-real-time.md) (Go) で今ある分を直しても、止める仕組みが無ければまた増える
(2026-09-05 に 262 の節 E で直した後、手書きのポーリングが 5 実装目まで増えた実例がある)。

規範 (`_claude/rules/avoid-wall-clock-assertions.md`) は文書だけで、機械では止めていない。

## 脅威モデル (_claude/rules/adversarial-review-own-safeguards.md §8。書いてから作る)

- **止めるもの**: テストを書く人 (Claude を含む) が、何かが起きるのを**秒数で待つ** `sleep` / `time.Sleep` を、理由を書かずに足すこと
- **止めないもの (検出しない形)**:
  - 本番の定数 (猶予・lease・ticker) を実時間で過ぎさせる形。テストには `sleep` が現れない (614 の主因がこれ)。review で見る
  - `perl -e 'sleep 60'` / `select(undef,undef,undef,0.5)` / `read -t` / `timeout` など、`sleep` という語を使わない待ち
  - fake のスクリプトを heredoc で書き出したファイルの中の `sleep` は、shell の検査では文字列として見えるので検出する (許可の印で通す)
- 許可は**行ごとの印に理由を書く**形にし、印の理由を review が読む (検査は理由の妥当性を判定しない)

## 対応方針

### shell (`tests/` 配下の `*.sh` / `*.bats` / `*.zsh` と `tests/lib/`)

- `scripts/check_test_sleeps.sh` を足し、`make test-lint` に入れる (前例: `scripts/check_pipefail_grep_q.sh`)
- 語としての `sleep` (`\bsleep\b`。`sleep "$x"` / `command sleep` / `/bin/sleep` / `sleep 1 &` を含む) が実行位置に現れる行は、
  次のどれかでなければ落とす。🚨 `sleep +[0-9.$]` の形で探すと `sleep "$x"` を落とす (613 の最初の集計で 5 行漏れた)
  1. `tests/lib/wait_until.sh` の中 (ポーリングの刻みの唯一の実装)
  2. **直前の行**に `# sleep-ok: <分類>: <理由>` の印がある。分類は 613 の語彙 (`dummy` / `window` / `negative` / `stub` / `tick`) に限る。
     行末に置かない: `\` で続く行 (`test_mutate_verify.sh:753`) や heredoc の中 (fake の本体・生成する Makefile。
     `test_run_make_targets_parallel.sh:30,34,92` / `test_schedule_keys.sh:173-176`) では、行末の `#` が継続を壊すか生成物の一部になる
  3. heredoc で書き出す fake の中の行は、heredoc の開始行の直前の印 1 つでまとめて許す (印の書き方は着手時に決める)
- 検出しない形 (上の脅威モデルに足す): `perl -e 'sleep 60'` のような別言語の文字列の中、パイプの左側で入力を保持するだけの `( sleep 8 | ... )`
  (`test_reap_orphan_servers.sh:254`)。どちらも拾うと許可の印が膨らむだけで、待ちを止めない
- bats (`tests/*.bats`) も対象。`codex_fanout.bats:260,270` の手書きの刻みは、`wait_until.sh` を source させるか印を付けるかを決める
- 613 の残り (DUMMY 39 / NEGATIVE / STUB ほか) に印を付けてから検査を入れる (入れた時点で 0 件にする)
- 印の付いた件数を出す (`検査 N 行、印つき M 行`)。0 件の走査を緑にしない (verify-execution-not-just-exit-code)

### Go (`src/*/` の `*_test.go`)

- forbidigo に `^time\.Sleep$` を足し、`*_test.go` に限る (本番コードの `time.Sleep` は対象外)。v2 では
  `path-except: _test\.go` + `text: <この規則の msg>` の exclusion で書ける (前例: `src/glogx/.golangci.yml` の `path-except: tui\.go`)。
  `text:` で絞らないと、既存の `fmt.Print` / `time.Now` の規則まで巻き込む
- 🚨 glogx は `path: _test\.go` で forbidigo を**丸ごと**止めている (dupl / unparam と同じ exclusion)。足すだけではテストに効かないので、
  この exclusion を既存の規則の `text:` に絞り直す。他の 6 module のうち pro-con / doctor / restartable は
  forbidigo の設定を持つので規則を足し、lockman / process_supervisor / chromecookie は `.golangci.yml` に forbidigo が無いので有効化から要る
  (2026-10-02 に `grep -L forbidigo src/*/.golangci.yml` で確認)
- 許可は `//nolint:forbidigo // sleep-ok: <分類>: <理由>`。ポーリングの helper (module ごとに 1 つ。614 の 10) の中だけは exclusions で許す
- 子プロセスの `sleep` (`exec.Command("sh", "-c", "sleep 60")` 等) は forbidigo では見えない。ほぼ全部が DUMMY なので対象外にする
  (上の「検出しない形」に足す)

### 検査そのものの確かめ (§1.5)

- 印の無い `sleep 0.5` を 1 行足した fixture で red / 印を付けて green / `wait_until.sh` の中は green / コメント行は green を、検査のテストに置く
- Go は印の無い `time.Sleep` を足す変異で `make lint` が red になることを、forbidigo を足した各 module で確かめる
- CI で走ることを、検査名がログに出ることで確かめる

## 受け入れ条件

- [ ] 613 / 614 の残りに印を付け、検査を入れた時点で違反 0 件
- [ ] shell の検査と、その fixture のテスト (red / green の 4 形)
- [ ] Go の forbidigo を時間を待つテストのある module (lockman / pro-con / glogx / doctor / process_supervisor / restartable / chromecookie) に入れる
- [ ] `_claude/rules/avoid-wall-clock-assertions.md` に「検査が止める・印の書き方」を 1 行足す (入口のドキュメント)

## 関連ファイル

- `_claude/rules/avoid-wall-clock-assertions.md` / `tests/lib/wait_until.sh`
- `scripts/check_pipefail_grep_q.sh` (検査の前例) / `src/glogx/.golangci.yml` (forbidigo で `time.Now` を止めている前例)

## 進捗

- 2026-10-02 方針を 1 点変えた: **Go も forbidigo ではなく `scripts/check_test_sleeps.sh` で見る** (`time.Sleep(` に同じ `sleep-ok:` の印を要求)。
  理由: 規則と印を 1 つにできる / glogx と pro-con の `.golangci.yml` はテストで forbidigo を丸ごと外しており、7 module の lint 設定
  (うち 3 module は forbidigo 自体が無い) を崩さずに済む / `lint.yml` は paths の絞り込み無しで毎 push `make test-lint` を回すので CI で必ず走る
- 実装: `scripts/check_test_sleeps.sh` (`make test-lint` の `test-test-sleeps`) と `tests/scripts/test_check_test_sleeps.sh` (fixture 22 件:
  印の無い 5 形を落とす / 同じ行・直前の行の印で通す / 語彙に無い分類・理由なし・2 行上の印は通さない / heredoc は開始行の前の印でまとめて通し、
  印が無ければ本文も落とし、heredoc の後ろの行は通さない / コメント行・別の語・wait_until.sh を数えない / 対象が下限未満なら失敗 / Go の 4 形 /
  本物の tests/ と src/ が通る / Makefile の配線)
- shell の印付け: 108 行のうち 97 行に印 (sonnet。印を取り除くと元と完全に一致することを main が機械で確認)、残り 11 行のうち 7 行は
  `tt_wait_until` に寄せ (schedule_keys の sleeper 待ち・smooth_scroll の 4 箇所・codex_fanout.bats の 2 箇所)、4 行は理由つきの印
  (smooth_scroll の静止判定のサンプリング 2 行・test_tt は sleep を関数で潰しているので helper が使えない・warmup の runs_within は判定器)
- 入口: `_claude/rules/avoid-wall-clock-assertions.md` に 1 行 (検査が止めること・印の書き方)
