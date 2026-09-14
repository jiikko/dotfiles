# 372 refactor: disk.Report/Result の導出フィールドを「所有者を迂回して」書く経路を機械で止めていない

- 起票: 2026-09-13
- 種別: `refactor` (現状は全経路が正しい。次に足す人への防御)
- 出典: audit の `encapsulation` (E2 不変条件の外部維持) で見つけた 2 件を
  `disk.Report.WithResults` / `disk.Result.WithItems` へ寄せた commit の残課題

## 何が残っているか

`Report.Total` は `Results` の導出値、`Result.Size` は `Items` の導出値 (例外: Items を持たない
Reused / FromSnapshot は Size を保つ)。この 2 つの整合は
**`WithResults` / `WithItems` を通ったときだけ**保たれる。

寄せた結果、いま導出フィールドへ直接書く箇所は所有者パッケージ内の以下だけ:

- `src/doctor/disk/report.go` の `WithResults` / `WithItems` (唯一の出典)
- `src/doctor/disk/scan.go` の `Items = append(...)` / `Size += ...` (生成時。対で書いている)

**ただし「次に足す人が `rep.Results = xs` と直接書く」のを止めるものは無い**。build もテストも通り、
合計だけが古いまま残る (silent)。寄せる前の glogx 側 3 箇所がまさにその形だった。

## 発火条件

- 新しい経路が `Results` / `Items` を差し替えて `WithResults` / `WithItems` を通さないとき
- 症状は「行は正しいのに画面上部の合計だけ古い」「消したのに減らない」

## 修正方向 (どちらか)

1. **ソース走査テスト**を足す (前例: `src/glogx/issues_rows_setter_test.go` が
   `issuesView.rows` の直接代入を AST で止めている)。所有者パッケージの外から
   `.Total =` / `.Size =` / `.Items =` を書く形を違反にする
2. **導出値をフィールドから外す** (`Total()` / `Size()` をメソッドにする)。JSON の
   スキーマ (`json:"total"` / `json:"size"`) が snapshot の保存形式なので、
   互換のために出力時だけ埋める形になる。影響が大きい

🚨 1 を採るなら、それ自体が「自作の検査」なので
[`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) の
手順 (脅威モデルと「検出しない形」を先に書く / canary は本走査と同じ関数を通す / 変異で red を見る) を
通すこと。`issues_rows_setter_test.go` のヘッダがその作法の実例になっている。

## 進捗

- 2026-09-13 `refactor(disk): 導出フィールドの整合を Report.WithResults / Result.WithItems へ寄せる`
  — 呼び出し側 8 箇所 (disk 3 / glogx 5) を所有者の API へ寄せ、glogx 側で導出フィールドを手で書く
  箇所は 0 件になった。変異 3 本 (例外ガード除去 / Size 引き直し除去 / Total 引き直し除去) で red を確認。
  **本 issue が言う「迂回の機械的な禁止」はこの commit には入っていない**

- 2026-09-14 `test(disk,372): 導出フィールドの迂回をソース走査で止める` — 選択肢 1 を実装。

  **なぜ 1 か (2 と ruleguard を落とした理由)**

  - 選択肢 2 (`Total()` / `Size()` をメソッド化) は、所有者パッケージ外の参照が
    **Total 45 / Size 60 / Results 21 / Items 122 箇所** (実測 2026-09-14。`grep -rn '\.<name>\b'`
    の doctor(disk 外) + glogx の合計)。JSON スキーマの互換も要るので見送った
  - **ruleguard は使えなかった** (実験で確定。glogx は既に ruleguard が配線済みなので、
    doctor 側に何も作らずに試せた):
    - `m["x"].Type.Is("disk.Report")` も `("doctor/disk.Report")` も**跨モジュールの型を
      解決できず**、マッチ 0 件 (ルールセットのロード自体は成功していた)
    - ルールファイル `gorules/rules.go` に `_ "doctor/disk"` を import すると、
      **ルールセットのロードが落ちる** (全ファイルに
      `ruleguard: execution error: used Run() with an empty rule set` が出る)
    - 同様に `m.File().Name.Matches(…)` と `Type.Underlying().Is("struct{...}")` も
      ロードを落とす (go.mod の `dsl` ではなく golangci-lint 同梱の go-ruleguard が評価するため、
      `go vet -tags ruleguard ./gorules` が通ってもロードできるとは限らない)
    - 既存の `toastEncapsulation` が動いているのは**純粋に構文的** (`$_.toast.text`) だから

  **入れたもの**

  - `src/doctor/disk/derived_fields_bypass_test.go`: 所有者パッケージ (disk) の**外**かつ
    `doctor/disk` を import しているファイルを AST で走査し、`<disk.Report>.Results / .Total` と
    `<disk.Result>.Items / .Size` への代入 (複合代入・`++` を含む) を違反にする。
    型は go/types に頼らず「struct フィールド宣言の表 + 関数内のローカル束縛」で近似する
    (`sn.Disk.Results` / `v.diskRep.Results` のようなフィールド経由の owner を解くため)。
    脅威モデルと「**検出しない形**」はファイル冒頭に書いた
  - 実コードの迂回 **5 箇所**を `WithResults` / `WithItems` へ寄せた
    (doctor 1: `cmd/diskdoctor/exit_test.go` / glogx 4: `doctor_view_test.go` x3 +
    `doctor_delete_plan_target_test.go`)。**走査の結果と手作業の grep の全数勘定が一致**
  - `.github/workflows/src_doctor.yml` の paths に `src/glogx/**` を追加。
    🚨 **これが無いと「glogx だけ変えた push」で検査が 1 度も走らない** (下の M2 がその形)

  **実測 (2026-09-14)**: 走査 .go 270 件 / `doctor/disk` を import 18 件 / 型を解決できた参照 117 件 / 違反 0 件。
  誤検出しないことを確かめた実在の同名フィールド: `disk.EntryOutcome.Items` (glogx `doctor_delete.go`)、
  `doctorDiskCache.Total` (glogx `doctor_cache.go`)、`docker` パッケージの `Group.Items` / `.Size`
  (docker は `doctor/disk` を import しないので射程外)

  **変異 6 本すべてで red を確認** (段ごとに 1 本。予測した assert と一致。全変異でビルド可を確認):

  | 変異 | 落ちた assert |
  |---|---|
  | M1 `exit_test.go` の修正を旧実装へ戻す (テスト側の迂回) | 違反検出 (1 件) |
  | M2 glogx の **production** に `v.diskRep.Results = nil` を植える | 違反検出 (1 件) |
  | M3 判定 (`check`) を殺す | canary の検出が 0 件 (期待 12) |
  | M4 owner と同名のフィールド (`Disk string`) を足して衝突させる | owner フィールド表に `Disk` が居ない |
  | M5 走査根を `.` に壊す (空振り) | 走査した .go が 18 件 (下限 100) |
  | M6 本走査へフィールド表を渡さない (配線外し) | 表あり = 表なし (表が判定に効いていない) |

  M5 は最初「owner 表の assert」で落ちており (空振りすると表も空になるため誤った診断が出る)、
  **下限を owner 表の assert より前へ移した**。M6 は閾値では捕まらなかったので、
  「表あり > 表なし」の比較に置き換えた (実測値が動いても腐らない)。

- 2026-09-14 `test(disk,372): 敵対的レビュー 1 周目の指摘 7 件を塞ぐ`

  read-only の敵対的レビュー (opus 1 体) が**実測つき**で指摘を出した。裏を取って採用したもの:

  | 指摘 | 中身 | 対応 |
  |---|---|---|
  | P1-1 | `rhsKind` が `kindOf` の真部分集合で、`rep := *v.diskRep` / `p := &rep.Results[0]` / `rs := rep.Results` が全部すり抜ける | `rhsKind` を `kindOf` へ委譲して統合 |
  | P1-2 | 複合リテラル `disk.Report{… Results: xs}` が丸ごと射程外。production の唯一の構築点から `.WithResults` を剥がしても検出できない | **production だけ**検出 (テストは射程外。実測: テスト 137 / production 1) |
  | P1-3 | `rep.Results[i] = r` の要素差し替えが未検出。`= r.WithItems(xs)` と書けるので所有者 API を通しているように読める | `IndexExpr` の LHS を検出 |
  | P1-4 | `rep` (`doctorDiskEvent.rep *disk.Report`) が `doctorSvcMsg.rep svc.Report` と名前衝突して**既に表から落ちていた** | 盲点として `knownLost` に固定 + ヘッダに明記 (型解決なしには塞げない) |
  | P2-1 | `addField(fn.Recv)` が構造的に死んでいる (Go は他パッケージの型にメソッドを定義できない) | 削除 |
  | P2-2 | ヘッダの「検出しない」が実装と**両方向**にずれていた (別名変数経由は実際は検出する / ②③が抜けていた) | 宣言を実装に合わせた |
  | P2-3 | 走査根 (`src/`) と CI の paths (3 dir) が食い違う。将来 lockman 等が consumer になると素通り | consumer の module を assert |
  | P2-4 | 所有者パッケージの除外が**一度も発火していなかった** (`../disk` と `../../doctor/disk` を比較) | root 基準へ修正 (走査 270 → 252 件) |

  却下・記録に回したもの: P2-5 (関数内で `rep` を `disk.Report` と `svc.Report` に使い分けると
  誤検出しうる) は今日 0 件で、スコープ追跡には go/types が要るので**既知の誤検出リスク**として
  ヘッダに宣言。P3 の未検出形 (埋め込みフィールド / `new(T)` / 型スイッチ / `map[string]*disk.Report`
  の要素 / パッケージレベルの `var f = func(...)` / `copy()`) は「検出しない形」へ追記。

  canary を 12 → 22 件へ拡張し、**同じ canary を `_test.go` 名でも通して 20 件**
  (複合リテラル 2 件だけが落ちる) を固定する合格側の canary を足した。

  **変異をさらに 6 本追加し、累計 12 本すべてで red を確認** (全変異でビルド可):

  | 変異 | 落ちた assert |
  |---|---|
  | M7 production の `.WithResults` を剥がして複合リテラルで埋める | 違反検出 |
  | M8 `v.diskRep.Results[0] = disk.Result{}` を production に植える | 違反検出 |
  | M9 `cp := *v.diskRep; cp.Results = nil` を production に植える | 違反検出 |
  | M10 `knownLost` を空にする | `rep` が衝突で落ちた (盲点の宣言が消えた) |
  | M11 `src/lockman` を consumer にする | CI の paths に `src/lockman/**` を足せ |
  | M12 複合リテラルの検出を殺す | canary 20 件 (期待 22) |

  **実測 (修正後)**: 走査 .go 252 件 / import 18 件 / 型解決できた参照 161 件 / 違反 0 件。

- 2026-09-14 `test(disk,372): 敵対的レビュー 2 周目の指摘 6 件を塞ぐ` ほか

  2 周目は「1 周目の指摘に対して**新設した機構**」を攻めさせた ([`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §7)。

  | 指摘 | 中身 | 対応 |
  |---|---|---|
  | P1-1 | フィールド表が名前だけで型を決めるのに `r` / `sel` を採っていた。**glogx のどこかに `r` という名前のフィールドを 1 つ足すだけで doctor のこの検査が落ちる** (consumer に置けば衝突 assert、非 consumer なら誤検出。逃げ場が無い) | 4 文字未満は採らない。採らなかった名前 = 盲点として assert |
  | P1-2 | `[]disk.Result{{Items: its}}` は要素の型が省略されるので素通り (Size が 0 のまま残る最も自然な書き方) | 外側の型から降ろして見る |
  | P1-3 | module の allowlist をテスト側に書いていたので、**yml から `src/glogx/**` を消す退行**を止められない | yml を読んで push / pull_request の両方に居ることを確認 |
  | P2-1 | `owners` が単調で `var rep svc.Report` の再宣言を跨いで誤分類。glogx の `start()` が実際にその形で、`svc.Report` に `Total` が足されたら誤検出が出る状態 | 再宣言と型を追えない `:=` で束縛を消す |
  | P2-2 | ②の型ガードを外す変異が緑で生存 (canary に非 disk 型の `.Items[0] = …` が無かった) | 否定 canary を追加 |
  | P2-3 | パッケージレベルの `var rep disk.Report` への書き込みが素通り | 先に集める |
  | P3-2 | `src/` 直下の .go を module 名として扱う | 分割の前提を修正 |

  **塞がず宣言したもの**: 別名経由の要素差し替え (`rs := rep.Results; rs[0] = x`) と `*p = x`。
  ローカルに組んだ `[]disk.Result` (実測 75 箇所。`out[i] = r` は正当な構築) と区別できず、
  広げると誤検出になる。ヘッダの「検出しない」へ書いた。

  **レビューからの訂正を 1 つ受け入れた**: 「`rhsKind` が `kindOf` の真部分集合」は正しいが、
  フィールド名による誤検出面は**統合で生まれたのではなく以前から在った** (統合は射程を広げただけ)。

  **変異を 6 本追加** (M13-M15 / M17-M19)。すべて予測どおり red。
  🚨 途中 2 件で「変異が当たっていない」(アンカー不一致) のに GREEN が出た。
  **第 3 の結果として扱って測り直した** (当たったものは diff の stat で確認)。
  M16 (`case vs.Type != nil` を殺す) は**等価変異**だった (default のループが同じ削除をする) ので、
  冗長な枝を消して default へ寄せ、差し替えの M19 で red を確認した。累計 18 本。

- 2026-09-14 `test(disk,372): 敵対的レビュー 3 周目の指摘 5 件を塞ぐ` ほか

  | 指摘 | 中身 | 対応 |
  |---|---|---|
  | P1-1 | **yml を見る assert 自身が false green だった**。生テキストの部分一致なので、①コメントアウト ②`paths:` → `paths-ignore:` (意味が真逆) ③paths 全撤去 + 説明コメントに文字列だけ残す、が**すべて緑** | `workflowPaths` でインデントを読み、`on.<trigger>.paths` の要素だけを取る。コメント行は捨て、`paths-ignore` は error |
  | P2-1 | 引数の束縛に長さフィルタも削除も無く、2 周目 P1-1 の誤検出が**変数側に残っていた** (`func(r *disk.Result)` の後に `for _, r := range rows` を回すだけで誤検出) | disk 型でない引数と range の束縛を消す |
  | P2-3 | フィールド表にテストの table のフィールド名 (`sel`) まで入っており、**テストを 1 つ改名しただけで「衝突」と誤診して赤くなる** | production からだけ集める。盲点 3 → 2 (`rep` / `r`) |
  | P3-1 | `map[K]disk.Result{…}` / `[N]disk.Result{…}` の要素リテラルが未検出 | `elemIsResult` で降ろす |
  | P3-3 | owner 表の pin が `Disk` / `diskRep` だけ | `diskResults` を追加 |

  **塞がず宣言し直したもの**: ブロック内で owner 名を `:=` でシャドウすると、その後の
  本物の書き込みまで見逃す。レビューの実測では**束縛を一切消さない状態でも本走査は違反 0 件**
  なので、今日の緑はこの見逃しに支えられていない。ヘッダの分類を「誤検出リスク」から
  「見逃す形」へ直した (実測される向きが逆だった = §8 の宣言と射程のズレ)。

  **変異 5 本追加 (M20-M24)。うち M22 / M23 が緑で生存**したので、否定 canary
  (`canaryArgRebind` / `canaryRangeRebind`) を足して red を確認した。累計 24 本。
  🚨 この過程で **`git checkout` による復元で未コミットの修正を 3 回自分で消した**
  ([`mutation-verify-new-tests.md`](../../_claude/rules/mutation-verify-new-tests.md) の
  「指摘を直したら変異の前に commit する」が名指ししている形)。retro 候補。

- 2026-09-14 `fix(doctor,372): 導出フィールドの走査テストを go test のキャッシュから外す` ほか

  **4 周目でブロッカーが出た。この検査は入れた時点では CI で何も守っていなかった。**

  `derived_fields_bypass_test.go` は doctor module の**外** (src/glogx と
  `.github/workflows/`) を読むが、cmd/go は testlog の open/stat のうち **module root の外を
  再チェックしない** (`$GOROOT/src/cmd/go/internal/test/test.go` の "Do not recheck files
  outside the module, GOPATH, or GOROOT root.")。つまり**キャッシュキーに入力が 1 つも入らない**。

  自分で実測 (2026-09-14):

  | 手順 | 結果 |
  |---|---|
  | warm 後に glogx へ本物の違反 (`disk.Report{Results: rs}`) を植える | — |
  | `go test -race ./disk/` | **`ok (cached)` rc=0** ← 緑 |
  | `go test -race -count=1 ./disk/` | **FAIL** ← 実際は検出できる |

  🚨 **最も当たりやすいのは、この検査が守りたい状況そのもの**: 「glogx だけを触った push」は
  doctor のソースを変えないので必ずキャッシュに当たる。`src_doctor.yml` の paths に
  `src/glogx/**` を足した意味が消えていた (ゲートとキャッシュ穴が同じ trigger で発火する)。

  対応: `src/doctor/Makefile` に `test-derived-fields` を足し、`-count=1` で回して
  **PASS 行が出たこと**まで確認する (exit code だけを見ない)。A-B-C で検証:
  A 正常系で証拠行が出る / B glogx に違反を植えると rc=2 (キャッシュが温まっていても) /
  C テストを改名して `-run` が空振りしたら落ちる。

  他に 4 周目で採用した指摘: `map[K]disk.Result` / `[N]disk.Result` の降ろしを**変異が生存**
  していた (canary に配列・map・`[]disk.Report` の 3 形を追加) / `workflowPaths` が `on:` に
  錨を打っておらず `jobs:` 下の `push:` を trigger と誤読していた / 「paths が消えた」と
  「構造として読めなかった」でメッセージを分けた (混ぜると yamlfmt をかけただけの人が
  1 往復する) / range の削除を `:=` のときだけにした。

  **変異 4 本追加、累計 28 本すべて red**。この回から**変異は repo の外のコピーで**当てた
  (`cp -R src .github`)。3 周目までに `git checkout` で未コミットの修正を 3 回消しており、
  コピーなら復元事故が構造的に起きない。

  **レビューはここで打ち切る** ([`adversarial-review-own-safeguards.md`](../../_claude/rules/adversarial-review-own-safeguards.md) §8 の stopping rule)。
  yml リーダは構文ゲートで迂回が原理的に無限 (手書き anchor / フロー形式 /
  `branches-ignore` / `jobs.<id>.if: false` …) なので、**脅威モデルをヘッダへ凍結**した:
  止めるのは「うっかり paths を消す / コメントアウトする」典型形だけで、読めない書き方は
  「読めなかった」と言って落ちるまでが射程。その先は review の責務。

## 残タスク

- [x] 1 と 2 のどちらを採るか決める → **1 (ソース走査テスト)**。2 を落とした理由は上の実測
- [x] 採った方を実装する
- [ ] **未検証**: 「glogx だけを触った push で doctor の CI が起動し、**かつこの検査が
      実際に走る**」ことの実 run 確認。paths filter とキャッシュ無効化を入れたところまでで、
      実際の run では確かめていない (この commit は doctor と glogx の両方を触るので、
      どちらの workflow も起動してしまい分離できない)。
      🚨 **run が在ることを確認しても足りない** (キャッシュで `ok (cached)` を返す形が
      あったため)。**trigger**: 次に glogx だけを触る commit が master へ載ったとき、
      `bin/ci-log -a <run-id>` で `src/doctor` の run のログに
      **`[derived-fields] … OK (キャッシュ無効で実行)` の行が出ているか**を見る
