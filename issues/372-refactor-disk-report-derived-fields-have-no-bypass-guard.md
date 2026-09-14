# 372 refactor: disk.Report/Result の導出フィールドを「所有者を迂回して」書く経路を機械で止めていない

> 🚧 着手中: セッション dotfiles-d0 (2026-09-14 claim)

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
[`adversarial-review-own-safeguards.md`](../_claude/rules/adversarial-review-own-safeguards.md) の
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

## 残タスク

- [x] 1 と 2 のどちらを採るか決める → **1 (ソース走査テスト)**。2 を落とした理由は上の実測
- [x] 採った方を実装する
- [ ] **未検証**: 「glogx だけを触った push で doctor の CI が起動する」ことの実 run 確認。
      paths filter を静的に足したところまでで、実際の run では確かめていない
      (この commit は doctor と glogx の両方を触るので、どちらの workflow も起動してしまい
      分離できない)。**trigger**: 次に glogx だけを触る commit が master へ載ったとき、
      `bin/ci-log -l` で `src/doctor` の run が出ているかを見る
