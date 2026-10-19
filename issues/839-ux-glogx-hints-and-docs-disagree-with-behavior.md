# 839 (ux): doctor の y・filer の入力中など、案内と文書が実際の動きと食い違う (P3)

起票日: 2026-10-19

## 概要

hint 行・トースト・README・UI ガイドが、実際の動きと違うことを言っている箇所。どれも compile もテストも通るまま黙って食い違う。

## 詳細

### 1. doctor の `y` の案内とトーストが、コピーする中身と違う (P3。今起きているが、影響は表示の文言)

- 該当: `src/glogx/internal/doctorview/doctor_view.go` の `doctorCopyHintLabel`、`DoctorView.HandleKey` の `y`、各行の `copyPath`、`tui.go` の `m.copyWithToast(m.doctorOv.CopyPayload(), "パスをコピーしました")`
- 案内はタブだけで決める (`TabDocker || TabSSD` なら「コマンドをコピー」、他は「パスをコピー」)。実際にコピーするのは行の `copyPath` で、Homebrew タブでは警告の見出し (`copyPath: summary`) やコマンド (`copyPath: act.cmd`) が入る。main のトーストはどのタブでも「パスをコピーしました」
  - Docker / SSD: 案内は「コマンド」、トーストは「パス」
  - Homebrew: 案内もトーストも「パス」、中身は警告文かコマンド
- `doctorCopyHintLabel` の doc は「押せる手の説明が嘘になると、押さなくなる」と書いており、その規範に反している
- 同じ形: 「Enter: 開閉」の案内は `v.tab != TabSSD` で決め、実際の動き (`expandableRow` が見る `row.hasDetail`) と別の判定
- 発火条件: 今、Docker / SSD / Homebrew タブで `y`。将来タブを足すと、`==` / `!=` の判定は exhaustive の検査に掛からず「パス」「Enter: 開閉」に分類される (221 のプロセス診断は「作らない方に倒す」で保留中なので、タブを足す予定の裏付けではない)
- 推奨: 行にコピーの種類 (パス / コマンド / 本文) を持たせ、案内とトーストの文言をカーソル行のその値から決める (`CopyPayload()` が中身と表示名を一緒に返す)

### 2. treefiler (`F`) の入力中も「F: 閉じる  q: glogx を終了」を出す (P3)

- 該当: `src/glogx/internal/filerview/filer_view.go` の `Hint`
- `/` (find)・`!` (コマンド)・`:` (パスへ移動) の入力中も案内が変わらない。その間 `F` / `q` は入力欄に入る (`OwnsKeys()` は true)。mac-lan-manager は同じ状態で `esc: 戻る` に切り替える。`docs/glogx-ui-guide.md` 5 節「入力中は案内を差し替える」に反する (issues viewer の 750 と同じ形)
- 3 つ目の案内「キーは src/treefiler/README.md」は、他の repo で glogx を開いたときに意味が無い。treefiler 自身に `? keys` がある
- 裏取り (監査の中で実施): 3 つのモードで描き、`F` → `q` の後の入力欄が `/ Fq` で treefiler が開いたまま

### 3. 更新の周期の説明が 3 通り (P3)

- U の箱の下端は「5分ごとに更新」(`usage.SharedFresh`)、R のダッシュボードの案内は「(毎分自動更新)」(`ratelimit_dashboard.go`)、README の `U` / `R` の行は「1 分ごとに自動更新」
- 実際の再取得の周期は 1 分で、`src/ratelimit/usage/shared.go` の `FetchShared` は 5 分のゲートより先に statusline の観測を読む (627 の後半の切り替え)。5 分の制限は予備の CLI の取得側だけ。**R と README の「1 分」は合っていて、疑わしいのは U の下端の「5分ごと」とその説明のコメント**。全部を 5 分に揃えると誤りになる (codex の反証レビューで、起票時の「Claude の値は 5 分ごと」という見立てを訂正した)
- 同じデータの呼び名も「usage」(U の題)・「残量」(hint の `R: 残量`)・「ratelimit」(ダッシュボードの題) と揺れている

### 4. README のキー表が段落で切れている (P3)

- 該当: `src/glogx/README.md` の「全画面 (`i` / `D` / `R` / `F` / `L`) を出したまま `C-g` …」の段落。`X` の行と `w` の行の間にあるので、その後の 4 行 (`w` / Ctrl-D・U / g・G / q・Esc・Ctrl-C・Ctrl-G) が表ではなくパイプ入りの文として描かれる

### 5. UI ガイドの `q` の例外に treefiler が無い (P3)

- 該当: `docs/glogx-ui-guide.md` 1 節は「`q` は 1 段戻る。issues viewer と status viewer は例外」と書くが、treefiler の `q` も glogx を終える (`filer_view.go` の `FilerQuit`、`docs/treefiler-spec.md` 0.1 節)

## 対応方針

- 1 は案内・トースト・中身を行の 1 つの値から決める形にする (他は文言と文書の修正)

## 関連ファイル

- `src/glogx/internal/doctorview/doctor_view.go` / `src/glogx/tui.go`
- `src/glogx/internal/filerview/filer_view.go`
- `src/glogx/internal/usageview/usage_overlay.go` / `src/glogx/internal/ratelimitview/ratelimit_dashboard.go`
- `src/glogx/README.md` / `docs/glogx-ui-guide.md`

## 進捗

- [ ] 1 doctor の y / Enter
- [ ] 2 filer の入力中の hint
- [ ] 3 周期の文言
- [ ] 4 README の表
- [ ] 5 UI ガイド
