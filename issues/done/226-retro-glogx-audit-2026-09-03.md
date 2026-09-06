# 226 retro: glogx 監査 2 本 (resource-leaks/dead-code → 219、設計 4 種 → 222) の振り返り

起票日: 2026-09-03
対象セッション: glogx を対象に `/audit` を 2 回 (品質 2 種 → issue 219 / 設計 4 種 → issue 222)。
成果: issues 219・222 の起票、`src/doctor/.golangci.yml` の新設と exhaustive 4 件の解消
(origin `ecf285a7`)、裏取り ID 突合テストの抽出穴の修正 (origin `36237473`)

## 1. 「テストが在る」を却下の根拠にして、そのテストの抽出範囲を読まなかった

設計監査で `diskVerifyCommands` のカタログ ID 分岐を見つけたとき、
`TestDiskVerifyCommandsIDsExistInCatalog` の存在を確認して**却下**した。実際にはその抽出が
`case "([a-z0-9-]+)"` で **`case "a", "b":` の 2 個目以降を拾えず、11 ID のうち 7 件しか
突合していなかった** (反証レビューが実測、こちらでも再現)。canary も `len(ids) < 5` だったので
7 → 5 まで壊れても落ちない。

**何が悪かったか**: テストの「主張」(関数名とコメント) を読んで、「**射程**」(抽出が実際に
何件を見ているか) を測らなかった。自分のルールは「テストの存在を安全の根拠にしない」と
言っているのに、**却下の側でだけそれを緩めた** (発見を起票するときは疑うのに、
却下するときは追認していた)。

**切り出し先の提案**: 既存 [`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md)
の「よくある『守っていないテスト』の形」へ 1 項追記
(「**突合・走査系のテストは、抽出が実際に何件を見ているかを数えてから根拠にする**。
canary の閾値が実際の件数より大幅に低いと、壊れても落ちない」)。
🚨 新規ルールは立てない — 発動点は既存の「テストを書く/根拠にする」と同じ。

## 2. linter の件数を production とテストで混ぜて「実測」と書いた

issue 222 の初稿に「production に 12 件」と書いたが、実際は production 10 件
(exhaustive 4 + 未導入 6) で、12 はテスト込みの数を production として書いたもの。
設定ファイルのコメントにも同じ混同を commit していた (次の commit で訂正済み)。

**何が悪かったか**: `uniq -c` の合計をそのまま転記し、**測った単位 (production / テスト) を
明示せずに「実測」の見出しの下に置いた**。

**切り出し先の提案**: 既存 [`perf-claims-need-measurement.md`](../../_claude/rules/perf-claims-need-measurement.md)
の「内訳の合計を全体の実測として書かない」項へ、**「集計の母集合 (production / テスト /
除外した linter) を数字と同じ行に書く」**を追記。あるいは却下 (1 の項と違い、
一般化する価値が薄いなら記録だけで足りる) — 判断はユーザーに委ねる。

## 3. 「repo の規約違反」という framing を、一次情報を引かずに書いた

issue 222 の初稿は「この repo が明文化した『網羅は実装で強制する』が doctor で効いていない」と
書いたが、`src/README.md` は「**`.golangci.yml` は任意**（無ければ既定 linter で運用）」と
明記していた。反証レビューが「これを引いておかないと次の読者は by design で閉じる」と指摘。

**何が悪かったか**: `src/glogx/CLAUDE.md` は読んだが、**一階層上の `src/README.md`
(モジュール共通の規約の正本) を読まなかった**。

**切り出し先の提案**: 既存
[`verify-design-intent-before-refactor.md`](../../_claude/rules/verify-design-intent-before-refactor.md)
のチェックリストへ「**対象ディレクトリの CLAUDE.md だけでなく、一階層上の README / 共通規約も
読む**」を追記 (「意図的に選ばれた設計でないか」の確認手順の一部)。

## 4. 却下: push が 2 回止まったこと

共有 working tree が並行セッションの未コミット変更で dirty だったため `git pull --rebase` が
拒否され、worktree に origin/master を出して自分の commit だけ cherry-pick する形で公開した
(他セッションの未 push commit は巻き込んでいない)。

**却下**: 同じセッション中に別セッションが `worktree-per-session.md`
(origin `b2becba1`) を入れており、既定が「セッションごとに worktree」へ変わった。
本項の教訓はそのルールに吸収済みなので、切り出し不要。

## 5. 記録だけ: `/audit` の 2 問目以降が無回答で流れた

`AskUserQuestion` を複数問まとめて出したとき、1 問目だけ回答され残りが返らなかった (2 回)。
実行モードの選択も流れたので `direct` を自分で決めて進めた (結果は妥当だった)。
**切り出し先なし** (ハーネスの挙動で、こちら側の規律で直せるものではない)。
次回同種の作業では**1 問ずつ出す**か、既定を宣言して進める形にする。

## 残課題

- [x] 項目 1〜3 の切り出し (2026-09-04 実施)。3 本とも**既存ルールへの追記**で足りた (新規ルールは立てていない):
      `mutation-verify-new-tests.md` に「走査・突合系のテストは実際に何件を見ているかを数えてから
      根拠にする」/ `perf-claims-need-measurement.md` に「数えたものの母集合を数字と同じ行に書く」/
      `verify-design-intent-before-refactor.md` のチェックリスト 1 に「一階層上の共通規約も読む」。
      本文へ足したのは合計 9 行で、実例・実測は同名の `rules-rationale/` へ置いた
- [x] issue 222 の残件 (未導入 linter / 語彙の一元化と突合テスト / Makefile のコメント) — 2026-09-04 に
      全部片付けて 222 を done へ送った。**一元化しただけでは 6 語のうち 3 語が誰にも pin されて
      いなかった**ことが変異検証で判明し、テストを 3 本足している (詳細は 222 の「決着」節)
- [x] issue 219 — **別セッションが実装済み** (`9d43a707` / `41769a28`、`1042fab5` で done へ)。
      `doctor_cache.go` は `writeAtomic` 経由になっていることをコードで確認した
- [x] **CI で exhaustive が退行を止めることを実測した** (2026-09-04)。ユーザー承認のうえ使い捨て
      ブランチ `tmp-ci-exhaustive-probe` で検証した:
      `src/doctor/disk/scan.go` の `scanEntry` から `case GuardProcessAbsent:` を外した commit
      (`5c44472e`) を push し、`gh workflow run src_doctor.yml --ref <branch>` で起動
      (この workflow の push トリガは master 限定なので dispatch を使った)。
      run `33827955439` の `doctor / lint` が **失敗**し、ログに
      `disk/scan.go:165:2: missing cases in switch of type disk.Guard: disk.GuardProcessAbsent (exhaustive)`
      と `* exhaustive: 1` が出た。変異は `go build ./...` が通る形 (第 3 の結果ではない)。
      検証後にブランチと worktree は削除済み。master には一切載せていない。
