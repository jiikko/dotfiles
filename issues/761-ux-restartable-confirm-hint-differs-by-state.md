# 761 (ux): restartable の確認の案内が、通常時と遷移中で別の文言になる (tuikit の版上げの副作用)

起票日: 2026-10-14

## 概要

issue 759 で restartable の tuikit の固定版を 99b256e39f08 → 5bf65d09 へ上げた。新しい版の `confirm.Dialog` は幅に合わせて案内を短い形
(`y/Enter:実行 n/Esc:取消`) に替える (非公開の `fitHint`)。一方 restartable の `internal/ui/view.go` の `transitionPanel` は、`confirm.Box` の行に
`confirm.HintYesNo` を直に並べるので、長い文言を板の幅で切った形 (`y/Enter: 実行   n/Esc: キャン…`) のまま。
同じ確認が、遷移中かどうかで 2 通りの文言で出る (敵対レビュー (回帰) が旧版と新版を幅 20/30/36/37 × 遷移の有無で描いて再現)。

## 詳細

- 劣化した画面は無い (版上げの前は両方とも長い文言を切った形で、通常時だけが良くなった)
- 幅 20 では新版でも `y/Enter:実行 n…` で n の意味が切れる (旧版の `y/Enter: 実行 …` と同じ)

## 対応方針

- tuikit の `confirm` に、案内を幅に合わせる関数を公開する (例: `FitHint(hint, width)`)。restartable の `transitionPanel` がそれを通す
- restartable は tuikit を版で固定しているので、tuikit の変更を push してから restartable の go.mod の版を上げる (issue 759 と同じ手順)

## 関連ファイル

- `src/tuikit/confirm/confirm.go` (`fitHint`)・`src/restartable/internal/ui/view.go` (`transitionPanel` / `ViewLinesAt`)
- 起点: 759。監査の記録: 760

## 進捗

- [ ] 未着手
