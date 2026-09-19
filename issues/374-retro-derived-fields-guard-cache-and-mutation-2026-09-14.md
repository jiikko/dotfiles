# 374 retro: 導出フィールドの迂回ガード (372) を入れたセッション

- 起票: 2026-09-14
- 対象セッション: 2026-09-14
- 成果物: `src/doctor/disk/derived_fields_bypass_test.go` (新設) /
  `src/doctor/Makefile` の `test-derived-fields` / `.github/workflows/src_doctor.yml` の paths 追加 /
  実コードの迂回 5 箇所の移送 / issue 372 を done へ

## やったこと

1. **ruleguard で書けないことを実験で確定させた**。`Type.Is("disk.Report")` / `("doctor/disk.Report")`
   はどちらもマッチせず、`gorules/rules.go` に `doctor/disk` を import すると**ルールセット全体が
   ロードに失敗する** (golangci-lint v2.5.0 同梱の ruleguard は cross-module の型を解決できない)。
   選択肢 2 (メソッド化) は call site 100 箇所超の書き換えになるので、AST 走査テストを選んだ
2. `src/` 配下の `.go` のうち `src/doctor/disk` の**外**で `doctor/disk` を import するものを走査し、
   ①代入 (`+=` / `++` / `*p` / `xs[i]` / フィールド別名を含む) ②要素の差し替え ③**production のみ**の
   複合リテラル、の 3 形を検出する。型解決は go/types を使わず、構造体フィールド名の表 + 関数内の
   ローカル束縛で近似
3. 実コードの迂回 5 箇所を owner API (`WithResults` / `WithItems`) へ移送
4. 敵対的レビュー **4 周**、変異 **28 本** (等価変異 1 本を含む)
5. 実測: 走査 252 ファイル / consumer 18 / 型解決 161 / 違反 0

## 気づき・反省

### 1. 🚨 module の外を読むテストは `go test` のキャッシュに乗らない (4 周目の主要指摘)

`cmd/go` の testlog は **module root の外**の `open` / `stat` を**再チェックしない**
(`$GOROOT/src/cmd/go/internal/test/test.go`: "Do not recheck files outside the module, GOPATH, or
GOROOT root.")。つまり `src/doctor` のテストが `src/glogx` を読んでも、**キャッシュキーに入力が
1 つも入らない**。実測: warm cache + glogx に違反を植える → `ok (cached)` rc=0。`-count=1` → FAIL。

最悪なのは、**このガードが狙っている状況とキャッシュが効く状況が一致している**こと
(glogx だけを触る push では doctor のソースが変わらない)。`make test` に配線して緑を見た時点では
「走った」ように見えていた。

- 直し方: `test-derived-fields` target で `-count=1` + `-v` で走らせ、
  **`--- PASS: <テスト名>` の行が出たこと**を grep で確認する (`-run` のパターンが腐って
  0 件実行になった場合も落ちる)。A/B/C の 3 条件で検証済み
- **切り出し先 (要判断)**: 発動点が「自分で検査を新設したとき」ではなく
  **「テストが自 module の外を読むとき」**なので、既存のどのルールとも違う。
  候補は ①`verify-execution-not-just-exit-code.md` に「キャッシュされた緑は実行の証拠ではない」節を足す
  ②新規ルール 1 本。CLAUDE.md の「既存への追記を既定にする」に従うなら ①だが、
  発動点が既存本文のどの節とも噛み合っていないので**ユーザー判断を仰ぐ**

### 2. 🚨 `git checkout -- <path>` で自分の未コミット修正を 3 回消した

`mutation-verify-new-tests.md` の「復元の作法」が名指ししている罠を、**そのルールを読んでいながら
3 回**踏んだ。発動点はいずれも「敵対レビューの指摘を直した直後」で、まさにルールが
🚨 付きで警告している瞬間。

4 周目からは **repo の外へ `cp -R src .github` したコピー**に変異を当てる形にして事故 0。

- 既存ルールは「変異は使い捨て worktree で」「指摘を直したら変異の前に commit する」と書いてあり、
  規範としては足りている。足りていないのは**実行できていない**という事実の方
- **切り出し先**: `rules-rationale/mutation-verify-new-tests.md` に
  「ルールを読んでいても 3 回踏んだ (2026-09-14)」を実例として追記する。本文は増やさない

### 3. 変異が当たっていないのに GREEN を 3 回読んだ

M14 / M17 は錨 (`func doctorSnapshotInCatalog(`) が別ファイル (`doctor_cache.go`) にあり、
`doctor_view.go` への置換が 0 件だったのに GREEN を出した。手順 1.6 の「diff を目視」を
人力でやっていたのが原因。

- 直し方: 変異スクリプト側に「`old` が対象ファイルに 1 箇所だけあること」の guard を入れてから
  再発なし。既存ルールの 🚨「手順 1.5 / 1.6 を人が覚えるのをやめ、変異ハーネス側に guard を置く」
  そのもの
- **切り出し先**: 既存ルールで足りている → **却下** (実例だけ rationale へ)

### 4. 変異が予測と違う assert に落ちた / 閾値が緩すぎた

- M5: 予測は「ファイル数の下限」だったが、実際は**所有者テーブルの assert** が先に落ちた
  (走査が空 → テーブルも空)。診断が誤誘導されるので、ファイル数・consumer 数の下限を
  テーブル assert より**前**へ移した
- M6: `resolved < 60` の閾値をすり抜けた。閾値をやめ、**フィールド表ありとなしで 2 回走査して
  `resolved > withoutFields`** を比較する形にした (絶対値でなく差で見る)
- どちらも `mutation-verify-new-tests.md` の既存項 (「当てる前に、最初に落ちる assert を予測する」
  「閾値は判別するか変異で確かめる」) が効いた例。**ルールの追加は不要**

### 5. CI の paths filter に穴があった

`src_doctor.yml` の paths に `src/glogx/**` が無く、**この走査テストが読む対象を変えても
workflow が起動しない**状態だった。走査テストを書いたことで初めて「doctor の CI が glogx に
依存する」形が生まれたので、既存の穴ではなく**自分が作った穴**。同じ commit で塞いだ。

- テスト側にも `workflowPaths` の assert を置き、consumer の module すべてが
  `push` と `pull_request` の両方の paths に載っていることを機械で固定した

### 6. 字句 gate の停止則を適用した

`workflowPaths` の yml 読みは「YAML パーサではなく行ベース」なので迂回が原理的に無限にある。
`adversarial-review-own-safeguards.md` §8 に従い、**脅威モデル (「うっかり paths を消す」を止める。
意図的な迂回は review の責務) と「検出しない形」をヘッダに凍結**してレビューを打ち切った。
迂回指摘を追い続けていたら収束していない。

## 残タスク

- [x] **未検証**: glogx だけを触る push で `src/doctor` workflow が起動し、**かつ検査が実際に走る**
      ことの実 CI 確認。**trigger**: 次に glogx 単独の commit が master に載ったとき、
      `bin/ci-log -a <run-id>` で `[derived-fields] … OK (キャッシュ無効で実行)` がログに出るか見る。
      run が存在するだけでは不十分 (それが 4 周目のブロッカーの本質だった)。
      → 記録の正本は `issues/done/372-*.md` の残タスク節。ここは retro 側の写し
- [x] 上記「気づき 1」の切り出し先 (既存ルールへの追記 / 新規ルール) をユーザーが判断する
- [x] 「気づき 2」「気づき 3」の実例を `_claude/rules-rationale/mutation-verify-new-tests.md` へ追記する

## 決着 (2026-09-19)

Fable サブエージェントの独立判定を踏まえて決定した。残課題なし。

- 1 (+ 5 の paths filter): `verify-execution-not-just-exit-code.md` に「キャッシュされた緑と path filter は鍵に数えない入力の変化を見ない」節を新設 (Go 固有に書かず一般化)
- 2: 本文の「復元の作法」を人が守れない実績として `mutation-verify-new-tests.md` の「変異ハーネス側に guard」へ「未コミット差分があれば拒否」を追記 (rationale 追記はしない)
- 3・4・6: 却下 (既存ルールが効いた / 足りている)
- CI 実走確認: 正本は done/372 の残タスク。retro 側からは外す
