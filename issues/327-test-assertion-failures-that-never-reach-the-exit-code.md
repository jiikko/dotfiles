# test: `✗` を出しても exit code に出ないテストがあり、失敗が CI から不可視になっている

起票日: 2026-09-07
カテゴリ: test
優先度: 中（**失敗が緑として集計される**形。issue 139（skip が `[ok]` になっていた）と同じクラス）
出典: issue 306 の修正中に実測で踏んだ

## 何が起きているか

`make test` のテストランナー（`Makefile:run_tests`）は **exit code しか見ない**:

```make
if "$$t"; then :; else rc=$$?; if [ "$$rc" -eq 77 ]; then ... skips ... else ... fails ... fi; fi
```

出力の `✗` は**一切見ていない**。したがって「`✗` を printf するだけで非 0 にならない」
アサーションは、**失敗しても `[ok]` に集計される**。

## 実測（2026-09-07。これが唯一の確実な証拠）

`tests/zshrc/av1ify/test_av1ify_prefetch.sh` に、issue 306 の看取りを無効化する変異を当てた:

```
変異後の出力: ✗ 正常終了後に prefetch が 1 個残った (issue 306)
変異後の rc : 0        ← ランナーはこれを合格として集計する
```

このファイルには **`✗` を出す分岐が 39 箇所**あり、`exit 1` は 7 箇所とも
`cd "$TEST_DIR" || exit 1`（ディレクトリ移動の失敗）だけ。
**アサーション failure から非 0 へ至る経路が 1 本も無い。**

issue 306 の commit では、私が足した Test 18 **だけ**を `exit 1` にして塞いだ。
残る 38 箇所はそのまま。

## 🚨 静的検査では母集合を確定できない（実際に外した）

`✗` を含むが非 0 の仕掛けが無いファイル、という grep で 115 ファイルを走査したところ
**2 件**（`test_video_health.sh` / `validate-mp4/test_validate_mp4.sh`）しか出ず、
**当の `test_av1ify_prefetch.sh` は漏れた**。`cd ... || exit 1` が正規表現に当たってしまうため。

→ **「`exit 1` があるか」では判定できない。** 判定できるのは
「**アサーションを 1 つ壊したときに rc が非 0 になるか**」という実験だけ。

## 推奨対応（順序つき）

1. **まず `test_av1ify_prefetch.sh` の残り 38 箇所**を、失敗が rc に出る形へ揃える
   （`fail=1` を立てて末尾で `exit $fail`、または各所で `exit 1`。
   同 repo の `tests/scripts/test_run_make_targets_parallel.sh` が
   `fail=0` + `bad()` + 末尾 `[ "$fail" -eq 0 ] || exit 1` の形を持っており、これが参照実装）
2. **母集合を実験で確定する**: 各テストファイルへ機械的に「必ず失敗する assert」を 1 つ注入し、
   rc が非 0 になるかを見る meta-test。緑のまま通ったファイルが対象
   - 🚨 これは**自作の検査**なので
     [`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md)
     が発動する。異常系（注入に失敗した / ファイルが構文エラーになった）を
     **「合格」に丸めず第 3 の結果として出す**こと
   - 全ファイルへ毎回やるのは重いので、**CI の別 lane か手動の棚卸し**に置くのが現実的
3. 恒久策として、テストの**書き方の規約**（`fail` カウンタ + 末尾 `exit`）を
   `tests/CLAUDE.md` に 1 行足す

## 🚨 「✗ を grep する」方向へ逃げないこと

ランナー側で出力を grep して `✗` を失敗にする案は、一見安そうだが採らない:

- 正常な出力に `✗` を含むテスト（否定の説明・fixture の中身）を誤って落とす
- **失敗の判定を出力の文字列に預ける**形になり、文言を変えた瞬間に静かに壊れる
  （[`mutation-verify-new-tests.md`](../_claude/rules/mutation-verify-new-tests.md) の
  「文字列を部分一致で pin していないか」）

直すべきは**テスト側が rc を返さないこと**であって、ランナーの判定基準ではない。

## 関連

- issue 139（`exit 77` を 0 と区別していなかったため、60 件の assert が消えたのに `[ok]`）—
  **同じクラスの先行事例**。あちらは「丸ごと skip」、こちらは「個別の失敗」
- issue 306（この問題を踏んだ経緯。Test 18 だけ塞いである）
- issue 323 / 314（「検査は在るが壊れても緑」ファミリー）
