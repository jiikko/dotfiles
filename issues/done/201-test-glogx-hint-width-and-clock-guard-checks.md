# 201 test: glogx に「hint 幅」と「時計の巻き戻し」の検査を足す (done から転用)

起票日: 2026-09-03
出典: `/audit` の lint-from-done (direct、2026-09-03。src/glogx 限定)
重要度: **P2** (候補 1 は現に 3 箇所が切れている) / P3 (候補 3)

**全件、現在のコードで穴が実在することを実測で確認済み** (main agent が独立に再現)。

## 候補 1 (最優先): hint 行が幅に収まることを、全 hint 生成箇所で検査する

出典 done: [155](155-bug-status-viewer-hint-exceeds-popup-width.md) /
[154 項目 4](154-retro-glogx-viewer-crossnav-2026-09-01.md) /
[121](121-bug-glogx-viewer-hint-says-close-but-quits.md)。同族に
[116](116-bug-glogx-issues-renderer-overflows-at-tiny-width.md)。
**155 自身が「git log 一覧の hint も同じ状態にある。未対応であることを明記しておく」と書き残している。**

### 既存機構では止まらない (実測)

hint 幅のテストは `issues_view_test.go:TestIssuesViewHintFitsPopupWidth` と
`status_view_test.go:TestStatusViewHintFitsWidth` の **2 画面ぶんだけ** (`grep -l HintFits` で確認)。
`.golangci.yml` にも `ruleguard.rules.go` にも hint 幅の検査は無い。

### 現に切れている箇所 (`dispWidth` で実測。popup の予算は `hintWidth` = 84-2 = **82 桁**)

| hint 生成箇所 | 実測幅 | 何が消えるか |
|---|---|---|
| `doctor_view.go:doctorView.hint()` | **112 桁** | `D/q/esc: 閉じる` と `(削除はまだできません)` |
| `tui.go` job パネル (カーソルあり) | **106 桁** | `h/q: 閉じる` |
| `tui.go` detailOv | **95 桁** | `Y: 詳細コピー` |
| `issues_view.go` 本文モード | 82 桁 | 境界ぴったり (frame の余白が変われば切れる) |
| `ratelimit_dashboard.go` | 70 桁 | 現状 OK (ただし無検査) |

**155 が定義した実害の形そのもの (「抜ける手段が案内から消える」) が doctor viewer と job パネルで
再現している。** main agent が `dispWidth(v.hint())` を実行して 112 桁を確認した。

### 実装の機構

**ソース走査型のテスト** (`vs16_literal_test.go` がほぼ同じ骨格なので流用できる)。

1. `go/ast` で `hint()` メソッドと `hintLine` への代入を列挙し、**文字列リテラルを抽出**して
   `dispWidth <= testPopupWidth-2` を assert
2. **列挙表は持たない** ([117](117-test-glogx-bench-metric-routing-unpinned.md) の
   「列挙すると兄弟を足したときに追随を忘れる = この検査が守りたい事故を検査自身が踏む」を適用)
3. **走査 0 件は fail** (同じく 115 / 117 の規律)

🚨 **偽陰性は残る**: `m.spinner()` の連結や `🚨 ghErr` の前置のような**動的合成は測れない**。
静的リテラルだけで上記 3 件が落ちるので費用対効果は成立するが、「これで hint の幅は保証された」
とは書かないこと。

🚨 **検査を入れた時点で 3 箇所が red になる**ので、`fitHintItems` (155 で作った既存イディオム) へ
寄せる実装作業とセットで進める。

## 候補 2: 永続キャッシュの鮮度判定に「負の経過 (時計の巻き戻し)」ガードを強制する

出典 done: [174](174-bug-doctor-toast-silent-when-clock-moved-back.md) (「**この 1 箇所だけが
非対称**」と書いている) / [194](194-bug-doctor-carry-ttl-relies-on-unvalidated-timestamps.md) /
[170](170-test-doctor-vacuous-tests.md) (`age < 0` を削っても全 green だった変異報告)。

### 非対称が実在する (実測)

**ガード済み 4 箇所**: `doctor_cache.go:131` (`at.After(now)`) / `:332` (`age >= 0`) /
`:416` (`age < 0 ||`) / `:676` (同) — いずれも今日までの doctor 系の修正で入った。

**無ガード 4 箇所** (すべて disk 上の JSON から読んだ時刻):

```go
cache.go:47            return now.Sub(e.FetchedAt) < cacheTTL(e.State)      // 未来なら永久に fresh
claude_version.go:132  now.Sub(c.FetchedAt) >= claudeVersionTTL
usage_cache.go:75      now.Sub(entry.FetchedAt) >= usageCacheTTL
doctor_cache.go:320    age := now.Sub(c.ScannedAt); age > doctorStaleAfter  // 負の日数が出る
```

**174 が問題視した非対称が、8 箇所中 4 箇所で残っている。** 壊れ方も 174 と同じ silent
(古い CI 状態 / 古い claude バージョンを永久に fresh として使い続ける)。

### 実装の機構

`waitdelay_discipline_test.go` と同型のソース走査テスト。

- 対象は `now.Sub(X)` / `timeNow().Sub(X)` のうち、**比較相手が `*TTL` / `*After` / `*Cooldown` を
  名に持つ定数**のものだけ。同一関数内に `< 0` / `>= 0` / `.After(` / `IsZero()` のいずれかを要求
- 除外は行内コメント `// clock: elapsed-only` (waitdelay の `subproc: no-waitdelay` と同じ作法)
- 走査 0 件は fail

🚨 **比較先の定数名で絞ることが要点**。素朴に `.Sub(` 全部を対象にすると、アニメーション経過
(`status_view.go` / `issues_view.go` / `zoom.go` / `issues_drawer.go` の 11 箇所以上) が全部
引っかかる。これらは `statusOpenDuration` / `appZoomDuration` など `Duration` 系なので、
定数名で絞れば自動的に対象外になる。`tui.go` の `keyRepeatGuard` (同一プロセス内なので
巻き戻し無関係) だけは除外コメントが要る。

## 候補 3 (小): `vs16_literal_test.go` に「走査 0 件で fail」を足す

出典 done: [115](115-refactor-glogx-statusfilter-list-duplicated.md) /
[117](117-test-glogx-bench-metric-routing-unpinned.md) (どちらも「走査 0 件も fail にした」) /
[198](198-test-glogx-vacuous-assertions-found-by-mutation.md) の「攻めたが見つからなかった範囲」が
**`vs16_literal_test.go` に同じ guard が無いことを名指ししている**。

実測: `waitdelay_discipline_test.go` と `width_test.go:171` は `checked == 0` guard あり、
**`vs16_literal_test.go:51` は `t.Logf` で件数を出すだけで fail しない**。3 本中 1 本が非対称。

母数が 3 本しかないのでメタテストは割に合わない。**`if len(pkgs) == 0 { t.Fatal(...) }` を
1 行足すだけ**でよい (難易度: 極小)。4 本目の走査テストが出たらメタ検査を検討する。

## 受け入れ条件

- [ ] 候補 1: 走査型の検査を足し、**現に切れている 3 箇所を `fitHintItems` へ寄せて green にする**
- [ ] 候補 2: 走査型の検査を足し、無ガード 4 箇所にガードを入れる (または除外コメントで意図を明示)
- [ ] 候補 3: `vs16_literal_test.go` に 0 件 fail を足す
- [ ] 各検査は**変異で red を見る**まで確認する (走査対象を空にする / ガードを外す)
- [ ] 走査型の検査を足したら、`tests/CLAUDE.md` か `src/glogx/CLAUDE.md` の「不変条件は lint / test が
      正本」の一覧に載せる ([`new-tool-requires-entrypoint-docs.md`](../../_claude/rules/new-tool-requires-entrypoint-docs.md))

## 転用しないと判断したもの (次の監査が同じ提案を再生成しないため)

- **「期待値を production の定数・式から作らない」(自己言及テスト)**: done で 4 回出た最多パターン
  (198 発見 4 の色定数 / 115 / 170 / 194) だが、**一般形は機械判定できない**。狭い代替
  (`_test.go` での `sgr.*` / `ansi*` 参照禁止) は正当な参照が 6 ファイル 43 箇所あり FP が大きすぎる。
  198 が入れた `TestAnsiColorsAreDistinctPerMeaning` が実害の中心 (色の意味衝突) を押さえている
- **兄弟 Msg の `gen` 欠落** (114) と **enum の手書きスライス二重管理** (115): どちらも
  **glogx では 1 件ずつ**。前者は「非同期 Cmd 由来の Msg か」を静的に判定できず (同期 Msg が 30 型以上)、
  後者は `exhaustive` の守備範囲外で ruleguard でも書けない。ルール化はまだ根拠不足
- **既存機構が既に止めているもの**: 空白連結 (`padViaPadSpaces`) / toast 内部状態
  (`toastEncapsulation`) / 幅エンジン二重化 (depguard + `TestNoSecondWidthEngine`) /
  `timeNow` シーム迂回・stdout 直書き (forbidigo) / `WaitDelay` 漏れ / VS16 リテラル /
  bench metric の兄弟遮蔽
- **「表示されるかを assert しろ」の一般ルール化**: [154 項目 4](154-retro-glogx-viewer-crossnav-2026-09-01.md)
  が明示的に却下している。候補 1 は規範ではなく**列挙漏れを自動導出する具体的な検査**の形に限定した

## 対応 (2026-09-03)

**3 候補すべて実施。各件で変異 → red を確認した。**

| 候補 | commit | 変異 → 結果 |
|---|---|---|
| 3 (走査 0 件で fail) | `397fded2` | 走査対象を存在しないディレクトリにする → red |
| 2 (時計の巻き戻し) | `675e1dce` | 4 箇所のガードを 1 つずつ外す → **4 本とも red** |
| 1 (hint 幅) | `e01f76cd` | doctor / job パネルを元の固定文字列へ戻す → **2 本とも red** |

### 候補 1 の結果 (実測)

`doctorView.hint(width)` を `fitHintItems` へ寄せた後の幅:

| width | 出力 | 残る項目 |
|---|---|---|
| 82 | 80 桁 | 移動 / 詳細 / パスをコピー / **閉じる** / **(削除はまだできません)** |
| 60 | 50 桁 | 移動 / **閉じる** / **(削除はまだできません)** |
| 40 | 39 桁 | **閉じる** / **(削除はまだできません)** |
| 20 | 15 桁 | **閉じる** |

どの幅でも「抜ける手段」が残る (優先度 1)。破壊操作の有無は誤解が高くつくので優先度 2 に置いた。

### 候補 2 の結果

無ガードだった 4 箇所 (`cache.go:fresh` / `claude_version.go` / `usage_cache.go` /
`doctor_cache.go` の staleness 注記) にガードを入れ、**走査テスト**で新しい鮮度判定の追随を
強制するようにした (`clock_rollback_test.go`)。走査は 9 件を検査している。

`doctor_cache.go` の staleness だけは「弾く」ではなく**文言を変える**形にした
(「N 日前」を負の数で出さず「診断時刻が未来。時計を確認してください」)。黙って注記を落とすと
「新しい診断」に見えるため。

### 🚨 テストを書く側で 3 回間違えた (すべて変異検証が捕まえた)

1. **走査テストが生ソースを見ており、コメント内の `age < 0` をガードと誤認**していた。
   コードから条件を消しても green だった (変異 3 / 4 が素通り)。コメントを除いてから
   判定する形に直した
2. hint の幅を測るとき、frame の**左余白 1 桁を含めて**測っていた
3. hint の予算を固定 82 と書いていたが、`m.hintWidth()` は frame 無効時 84。固定値をやめた

**1 は「守っているつもりの走査テスト」をそのまま commit するところだった。**
変異を当てなければ 3 件とも気づけなかった。

## 受け入れ条件

- [x] 候補 1: 走査型の検査を足し、現に切れていた 3 箇所を `fitHintItems` へ寄せて green にした
- [x] 候補 2: 走査型の検査を足し、無ガード 4 箇所にガードを入れた
- [x] 候補 3: `vs16_literal_test.go` に 0 件 fail を足した
- [x] 各検査は変異で red を見るまで確認した (計 7 本)
- [x] `src/glogx/CLAUDE.md` の「不変条件は lint / test が正本」に 2 本を追記した
      ([`new-tool-requires-entrypoint-docs.md`](../../_claude/rules/new-tool-requires-entrypoint-docs.md))
