# av1ify のテストハーネスが「どの assert が守っているか」を観測できない (最初の失敗でファイルごと中断する)

起票日: 2026-09-19
カテゴリ: chore / priority: **medium**
対象: `tests/zshrc/av1ify/test_helper.sh` の `assert_*`、`tests/zshrc/av1ify/test_av1ify_fps_mode.sh`
出典: [issue 394](394-bug-av1ify-vfr-passthrough-breaks-quicktime.md) の実装への敵対的レビュー (素通り観点)
反証レビュー: 実施。**下記の中断はレビュー時の実測**

## 問題 1: 最初の assert 失敗でファイルごと終了するので、変異検証がケース単位で判定できない

`assert_contains` / `assert_not_contains` は `bad()` を呼ばず `return 1` するだけで、
test_helper の `setopt err_exit` により**その場でスクリプトが終了**する。実測 (変異 = `0/0` ガード除去):

```
## Test 5: avg_frame_rate=0/0 (unknown) keeps the old args
✗ avg_frame_rate=0/0 is not flagged as VFR
   ← ここで終了。Test 5 の残り 2 assert (-fps_mode / -r) は実行されず、
     "=== Tests Completed ===" も "失敗 N 件" も出ない
```

rc は 1 になるので失敗自体は runner に届くが、**「どの assert が検出力を持っているか」が観測できない**。
issue 394 の commit message は「変異 4 本で red を確認」と書いているが、実際に各変異で red を出した
assert は**ファイルあたり 1 本ずつ**で、Test 1 の `-fps_mode` や Test 5 の `-r` は
**どの変異でも一度も実行されていない**可能性が高い。

`mutation-verify-new-tests.md` の「テーブル駆動なら全ケースが red になるか / 判定はケース名ごとの
pass/fail 一覧で行い、スイートの rc で読まない」に反している。

なお **e2e (`test_av1ify_vfr_e2e.sh`) 側は正しい**: `check_eq` は `bad()` 経由で FAIL_COUNT を積み、
EXIT trap が rc を格上げするので複数の失敗が畳まれない (1 変異で 2 件が両方表示されることを実測)。
問題は `test_helper.sh` の `assert_*` を使う側だけ。

## 問題 2: `0/0` fixture は「分母 0 は判定不能」の 1 点しか検査していない

`__av1ify_detect_vfr` のガードは `a <= 0 || b <= 0` なので、**分母 0 は分子も 0 のときだけ**捕まる。
`test_av1ify_fps_mode.sh` Test 5 が選んだ fixture (`0/0`) はちょうどその唯一の形で、
「分母 0 = 判定不能」という不変条件を主張しているつもりが、実際には `0/0` という 1 点しか
検査していない。実測 (現行 HEAD、変異なし):

```
MOCK_FPS=30/1 MOCK_AVG_FPS=24/0  →  ARGS: -fps_mode cfr -r 24.000
```

ガード本体を消す変異は Test 5 で red になるので「変異で確認済み」は成立するが、**射程の外は無防備**
(この穴が [issue 397](397-bug-av1ify-vfr-wrong-r-value-undetected-frame-loss.md) の b を素通りさせた)。

## 問題 3: e2e の「VFR 正規化のメッセージが出る」は配線を pin しない

変異で `elif` ブロックを殺しても、メッセージは検出側が出すので**緑のまま**通る
(他の 2 assert が本体を守っているので実害は無いが、**この assert を「配線の証拠」として数えないこと**)。
同じ形の false green は `test_av1ify_color_tags.sh` Test 5 として既に repo に記録があり、
`test_av1ify_fps_mode.sh` のヘッダはそれを引用しているのに、e2e 側で同じ形を作ってしまっている。

## 受け入れ条件

- [x] `assert_*` の失敗で**ファイルを中断させず**、失敗を数えて最後に一覧を出す
      (e2e の `bad()` + EXIT trap と同じ形へ寄せる)。中断させる必要がある「前提崩れ」は
      明示的に `exit 1` する側へ分ける
- [x] 変異検証の判定を「ケース名ごとの pass/fail 一覧」で行えることを、実際に 1 本変異を当てて確認する
- [x] Test 5 の fixture を `0/0` と `24/0` の 2 ケースへ広げる (Test 6 として追加) (issue 397 の修正後は `24/0` も非検出が正)
- [x] e2e のメッセージ assert に、配線を pin する assert (値の比較) を併置する ([issue 397](397-bug-av1ify-vfr-wrong-r-value-undetected-frame-loss.md))

## 指摘なしだった点 (記録)

- e2e は **CI で実際に走っている** (run 35357805630 = HEAD で `[ok]`)。`exit 77` が `[ok]` に化ける形は
  無く、Makefile の `run_tests_parallel` が `[skip]` として別集計し、`scripts/check_skip_exit_code.sh` の
  meta-gate もある
- `_validate_mp4.zsh` の dts 正規表現「追加 → 取り下げ」は綺麗に閉じている。production は改修前と
  完全一致 (コメントのみ純増)、テストは 1 ケースが反転して残り、**dts 再追加の変異で red** を実測。
  しかも同じ commit で fixture の `time=` を 1s → 10s に直しており、これをしないと `truncated` が
  先に落ちて「dts を見ていない」ことを証明できなかった (= 先に落ちる別 assert を潰した正しい変異設計)


## 結果 (2026-09-19)

`assert_*` を `bad()` + `return 0` へ変えたことで、**変異検証をケース単位で読めるようになった**。
issue 397 の敵対レビュー 6 周で、これが実際に効いた場面:

- 変異 M1 で Test 6 の **3 assert すべて**が red になることを観測できた (従来は 1 本目で中断)
- 「このテストだけが red になる変異」を 1 本ずつ特定する作業が 1 run で回せた
  (`awk '/^## Test /{t=$0} /✗/{print t" || "$0}'` で表になる)
- 逆に、それによって**「red は出るが、出しているのは狙った assert ではない」形**が 3 回見つかった
  (M8 / Test 70i / Test 70j)。中断する設計のままなら、どれも「変異で red = 守られている」と
  誤って記録されていた

全 509 呼び出しは条件文脈に置かれていないことを確認済み (`if` / `&&` / `||` / `$(...)` の中に
assert が在る箇所は 0 件) なので、`return 0` 化による誤った緑は生じない。
