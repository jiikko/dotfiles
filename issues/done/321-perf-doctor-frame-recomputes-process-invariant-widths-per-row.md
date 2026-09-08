# perf: doctor のフレームが「プロセス不変の値」を行ごとに再計算している（同型 4 件）

起票日: 2026-09-07
カテゴリ: perf
優先度: 中（🚨 severity を過大にしないこと。doctor の再描画は spinner tick と打鍵に律速され
**常時 60fps ではない**。20 KB/frame は Go の GC には軽い。修正が安価なので着手順は前の方でよい、
という位置づけ）
出典: /audit performance 2026-09-06（forge Minimum+）。クロスレビューが改善後の値まで実測

## ① `doctorMaxMarkWidth()` を行ごとに呼んでいる（本命）

`src/glogx/doctor_view.go:diskSection` の行ループが `doctorMaxMarkWidth()` を毎行呼ぶ。
この値は `src/doctor/disk/report.go:MarkVocabulary` から導出される**プロセス不変の定数**で、
`labelW` は `o.width` にしか依存しない**ループ不変式**。

### 実測（darwin/arm64・`-race`）

| | allocs | bytes |
|---|---|---|
| before | 600 | 83,122 |
| hoist 単独（call site で 1 回） | 577 | 65,641 |
| **memo 単独（`sync.OnceValue`）** | 579 | **63,174** |

**memo だけで -19,948 B/frame**（フレーム確保バイトの約 26%）。hoist と memo の両方を入れる。

### 🚨 memo 化の理由づけを間違えないこと

一次報告は「x/ansi の初期化順に依存しないよう初回使用時に確定させる」と書いていたが、
これは**不要な警戒**。Go は import 先の `init` を先に完了させ、`termwidth.go` の `init` 自身が
その旨を明記しており、`widthenv` は `RUNEWIDTH_EASTASIAN` 下でテストを走らせない。
**`sync.OnceValue` を使う判断自体は妥当なので、理由だけ差し替える**（「プロセス不変の値を
毎フレーム再計算しないため」）。

### 🚨 `issues/done/238` の制約を壊さないこと

一次報告は「`issues/` を `MarkVocabulary` 等で grep して 0 件」と書いていたが**誤り**で、
`issues/done/238-bug-doctor-disk-row-width-budget-ignores-gutter.md` が正面から扱っている
（9 箇所で言及）。238 の制約は
**「以前 5 語をハードコードして `🔎 未検証` が抜けたので、語彙から導出する形へ直した」**。

- **memo 化は導出性を保つので安全**
- **定数へ焼き直す形は 238 の回帰**

## ② `dockerMarkWidth` も同型

`src/glogx/doctor_docker.go` が同じ形（固定語彙からプロセス不変の幅を、群の行ごとに再計算）。
①と同じ commit で。

## ③ 同一フレームで `SumDeletable` が 2 回走る

`doctor_view.go:tabSummary`（tabDisk 分岐）と `diskSection` の両方から呼ばれる。

## ④ `diskSection` が毎フレーム `results` の完全コピーを作って `sort.SliceStable` し直す

`sorted := append(...)` + `sort.SliceStable`。結果は入力が変わらない限り同じ。

## ⑤（別モジュール）`excludedRootFor` が path 1 本ごとに 10 root すべてを `EvalSymlinks` し直す

`src/doctor/disk/guard.go`。🚨 **これは据え置きの判断もありうる**（未決着。research issue 324 参照）:
`EvalSymlinks` は**破壊的操作のガード**の一部で、
[`sandbox-real-destructive-test-apis.md`](../../_claude/rules/sandbox-real-destructive-test-apis.md) の
「実行の直前に取り直した値で判定する」に触れる。**正規化結果のキャッシュは TOCTOU の窓を広げる**
ので、走査（読み取り）経路と削除（破壊）経路で扱いを分けること。

## 対応 (2026-09-08)

commit `perf(321): doctor のマーク幅をメモ化し、予算を実測へ下げる`。

### ① メモ化 → **-19,950 B/frame (-24%)**。② は実測して**却下**

`doctorMaxMarkWidth` を `sync.OnceValue` にした。実測 (-race・count=3):

| | allocs | bytes |
|---|---|---|
| before | 598〜604 | 83,100〜83,184 |
| after | **575〜583** | **63,152〜63,249** |

監査の予測 (-19,948 B) と一致。予算も **612 → 591 / 86,000 → 65,200 へ下げた** (🚨 直前の値は 620 ではなく 612。
620 は 2 commit 前で、issue 323 で既に 612 へ下げてあった)
(緩いまま残すと「メモ化を revert しても緑」= 改善を守らない予算になる)。

🚨 メモ化の理由づけは監査の「x/ansi の初期化順に依存しないため」ではなく
**「プロセス不変の値を毎フレーム再計算しないため」**に差し替えた（監査の理由づけは不要な警戒）。
238 の「語彙から導出する」制約は保っている（`disk.MarkVocabulary()` を初回に舐める形。
定数へ焼き直していない）。その旨をコードのコメントにも書いた。

### 🚨 ②（`dockerMarkWidth`）は同じ手を当てて**効果 0** だったので revert した

敵対レビューが「② を守るゲートが無い（revert しても doctor-docker は緑）」と指摘した。
骨子は正しかったが、根拠として添えられた外挿（「disk から外挿して約 -6.6 KB」）は**外れ**で、
実測すると差が出ない:

| | allocs | bytes |
|---|---|---|
| memo あり | 569 / 570 / 571 | 56,134〜56,170 |
| memo なし | 566 / 570 / 575 | 56,104〜56,174 |

**効くのは呼び出し回数ではなく中身**だった: disk 側の `MarkVocabulary()` は呼ぶたびに
`[]Item` と `[]Result` を 6 件ぶん**構築する**ので、8 行 × それが -19,950 B の正体。
docker 側は `[]docker.Kind`（string 4 個）を作って `dispWidth` を回すだけで、しかも
群の行ごと = 4 回/frame しかない。**効果の無い修正は入れない**（CLAUDE.md「不具合対応の原則」）ので
revert し、この実測を `doctor_docker.go` の該当関数の直上に残した（次の人が同じ提案をしないため）。

### ⑥ `hoist` は入れなかった

issue ① は「hoist と memo の両方を入れる」だが、**memo 後は確保 0**（`sync.OnceValue` の
atomic load のみ）なので、call site をループ外へ出しても測れる差が出ない。
「両方」は memo の効果を知る前の記述なので、実測を優先した。

### ③④⑤ は据え置き（理由をコード直近に残した）

- **③ SumDeletable が同一フレームで 2 回** → 据え置き。`SumDeletable` は Result を舐めて
  int64 を足すだけで**確保を 1 バイトもしない**ので、フレーム確保への寄与は 0。
  まとめるなら `diskRep.Total` を使う形になるが、それは Total の鮮度（削除後に Report を
  作り直したか）に依存する — 「消したのに減らない」は実際に起きた形
- **④ 毎フレームのコピー + 整列** → 据え置き。確保は slice 1 本ぶんで、①②の -19,950 B に対して
  無視できる。メモ化すると「results が変わったか」の判定を持つことになり、走査中（1 件ずつ届く）と
  削除後に無効化し忘れる経路が増える
- **⑤ `excludedRootFor` の `EvalSymlinks`** → 据え置き。**破壊的操作のガード**の一部で、
  キャッシュは検査と実行のあいだの TOCTOU の窓を広げる（symlink を差し替えれば除外を外せる）。
  速くするなら「走査経路だけキャッシュし、削除経路は必ず取り直す」設計が要る — 需要が出るまで凍結

## 受け入れ条件

- [x] ①②を入れ、`TestFrameAllocBudget` の doctor-disk を実測へ**下げた**（620 → 591 / 86000 → 65200）
- [x] **変異検証**: memo を `func` へ戻す変異で doctor-disk が
      **回数 602 > 591・バイト 83,178 > 65,200 の両方で red**。
      🚨 1 回目の変異は `sync` が未使用になってビルド不能だった（第 3 の結果として扱い、
      import ごと外す「実際の revert の姿」で当て直した）
- [x] **敵対レビュー（opus・read-only）を通した**。P1「② を守るゲートが無い」は
      実測して②の revert で解決、P2「上限を動かしたのに『行ごと 1 確保』の変異を当て直していない」は
      当て直して 4 タブとも red を確認（doctor-docker は 581 vs 580 の差 1 なので、
      flake したら上限を緩める前に fixture を大きくする、をコメントに明記）、
      P2「`excludedRootFor` のコメントが実在しない経路を示唆」は
      「経路は 1 本で、`planDelete` が削除直前に `Scan` を回すためこの関数が直前判定そのもの」へ書き換え、
      P3「620 → 591 は事実誤り（直前は 612）」「26% は算術誤りで 24%」も訂正。
      レビューが「壊せなかった」と明記した観点: メモ化の不変条件（`dispWidth` の値がプロセス途中で
      変わる経路は無い）/ テスト間の汚染 / issue 238 の回帰 / `SumDeletable` の確保 0
- [x] 238 の「語彙から導出する」制約が保たれている（`MarkVocabulary()` を初回に舐める）
- [x] ⑤は据え置き。理由を `src/doctor/disk/guard.go:excludedRootFor` の直上に残した

## 関連

- `issues/done/238`（語彙から導出する形へ直した経緯。**着手前に読むこと**）
- `issues/done/270`（doctor フレームの遅延化。予算を締め直した先例）
- issue 323（予算が退行を観測できていない件。①の締め直しはそちらと同じ commit でもよい）
