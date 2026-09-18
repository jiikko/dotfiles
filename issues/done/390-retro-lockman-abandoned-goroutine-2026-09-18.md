# retro: issue 362 (見捨てた goroutine の副作用) — 検証が自分を 8 回止めた

起票日: 2026-09-18
カテゴリ: retro
対象セッション: issue 362 の実装 (commit `a75ffe3d..1fddd808`、14 commit)

## 何をやったか

`--io-timeout` で見捨てた goroutine が lock / 引き継ぎの目印を置いていく問題を実装で塞いだ。
実装・テスト・変異検証・実バイナリの A-B・敵対的レビュー 3 観点まで通した。
結果と残タスクは [362](../362-bug-lockman-abandoned-timeout-goroutine-leaves-lock.md) 本体。

## 気づき

### 1. 「安全機構を足すと、隣の安全機構のテストが vacuous になる」を 2 回踏んだ

- 1 段目 (作らない) のテストが 2 段目 (取り消す) に救われていた
- claim 直後の検査のテストが、後から足した rename 直前の検査に救われていた

どちらも**最終状態だけを見ていた**ため、前段を外しても後段が同じ状態を作る。
観測点を「後段へ進まないこと」に変えて解決した。

**切り出し先の提案**: [`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md) の
「よくある『守っていないテスト』の形」へ 1 項追記。発動点は「検査を新設したとき、
**隣の検査の変異を当て直す**」。既存の「段ごとに変異を当てる」(adversarial §1.5) は
*同時に書いた段* が対象で、**後から足した段が既存の段をマスクする**形は覆っていない。

### 2. 「変異が当たっていない / 弱い」を 3 回踏んだ

- M6: 実装を `switch` へ変えたらハーネスのパターンが陳腐化し、無音で当たらなくなった
- M3: 上記のマスクで緑になった
- lint 配線の確認: `foo=1; echo $foo` では SC2086 が出ず、「lint 対象外」と誤診しかけた

3 件とも**ハーネス側の guard (diff で当たったか確認) と、変異候補の事前確認**で拾えた。
既存ルールが要求している内容そのものなので、**新規ルールは不要**。

**切り出し先の提案**: 却下 (既存の [`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md)
の 🚨「手順 1.5 / 1.6 を人が覚えるのをやめ、変異ハーネス側に guard を置く」で足りている)。
ただし**変異候補を単体で先に検証する**(= その変異が本当に検出対象を作るか) は明文に無いので、
そこだけ 1 行追記の余地あり。

### 3. background タスクの「exit code 0」を make の rc と読み違えた

`make test > out 2>&1; echo "rc=$?"` を background で回し、ハーネスが報告した
`[exited with code 0]` を「テスト緑」と読んだ。実際は make が rc=2 で落ちており、
0 は**シェル全体**の終了コードだった。2 回目は `rc=` 行を直接読んで気づいた。

**切り出し先の提案**: [`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)
の「非同期・background の完了も『成果物』で判定する」節へ 1 行。
既存はパイプ終端の rc を扱っているが、**ラッパーの rc** (background ハーネス / `; echo $?` の
複合コマンド) は覆っていない。発動点は「background で回した検証の結果を読むとき」。

### 4. 自分の主張が 5 つ崩れた。全部「証拠が主張に届いていない」型

「exit 3 が exit 1 に化ける」/「②の効果は①として現れている」/「③の残余は原理的に 0 にできない」/
「取り消せなかったことは黙らない」/「probe は defer が消す」。
いずれも**もっともらしく、実際に確かめるまで自分では疑わなかった**。
外部の敵対レビュー (観点を分けた opus 3 体・直列) が全部出した。

**切り出し先の提案**: 却下 (CLAUDE.md「レビュー方針」の「主張は証拠ではない」と
[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §5 が
既に要求しており、**規律どおりやったから見つかった**。ルールの不足ではない)。

### 5. A-B の regime 特定に時間を使った。issue の過去の実測値は再現しなかった

本文の「`--io-timeout 1ms` で 35/450」は、①その後入った `minIOTimeout` 検証で弾かれ、
②両腕に同じ 1 行を当てて通しても**早すぎて goroutine が link() へ到達せず両腕 0** になる。
有効な窓 (3〜6ms) をスイープで探し直して初めて測れた。

**切り出し先の提案**: 新規 issue は不要。ハーネス
([`src/lockman/ab_abandoned.sh`](../../src/lockman/ab_abandoned.sh)) が毎回スイープする形にして
恒久化済み。**ただし「過去の実測値は環境と製品仕様の変化で再現しなくなる」**という一般則は
[`perf-claims-need-measurement.md`](../../_claude/rules/perf-claims-need-measurement.md) の
「測定条件を残す」の裏返しなので、同ルールへ 1 行追記の余地あり
(「再現できなかったら、まず測定条件が今も成立するかを確かめる」)。

## ぼやき

`os.Exit` は defer を走らせないため `serverNow` の probe と `tryPlace` の tmp が残る件は、
[issue 391](391-bug-lockman-os-exit-skips-defer-leaves-scratch.md) として起票した (2026-09-18)。

## 残課題

**(2026-09-18 完了) 切り出しは全て実施した。** 内訳は下記。

汎用の規範として切り出したもの (既存ルールへ追記):

- [x] **検査を 1 つ足したら、隣接する既存の検査の変異を当て直す** →
      [`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md)。
      後から足した検査が既存の検査の最終状態を肩代わりすると、既存のテストは緑のまま無検査になる。
      観測点を最終状態から「後段へ進まないこと」へ移す
- [x] **非同期で回した検証の結果は、ラッパーの終了コードで判定しない** →
      [`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)。
      複合コマンドを丸ごと非同期化すると、報告される rc は最後の要素のもので本体の失敗を隠す
- [x] **過去の実測値が再現しないときは、実装の退行より先に測定条件の失効を疑う** →
      [`perf-claims-need-measurement.md`](../../_claude/rules/perf-claims-need-measurement.md)。
      条件が失効していると両腕とも 0 が出て「差が無い」に見えるので、再現失敗を実装の評価に使わない

却下したもの (理由は本文の各節):

- [x] 「変異が当たらない / 弱い」→ 既存ルールの「変異ハーネス側に guard を置く」で足りる。
      ただし**変異候補が本当に検出対象を作るかを単体で先に確かめる**は明文に無いので、
      次に同じ形を踏んだら 1 行追記する (今回は 1 例のため見送り)
- [x] 「自分の主張が崩れた」→ 規律どおり外部レビューを通したから見つかった。ルールの不足ではない
