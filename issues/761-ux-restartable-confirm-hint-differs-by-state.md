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

- [x] tuikit の `confirm` に `FitHint(案内, 幅)` を公開した (Box に渡す幅を受け取り、Dialog と同じ規則で短い形に替える)。幅 1〜60 で
  「Box + FitHint の板」が「Dialog の板」と一致することをテストで見る。README の confirm の行にも足した
  — `feat(tuikit/confirm): Box に案内を並べる使う側のために FitHint を公開する`
- [x] restartable の tuikit を cb9e1aaf (FitHint の版) へ上げ、`transitionPanel` の案内を `confirm.FitHint` に通した。幅 20/30/36/37/80 で
  遷移中と通常時の案内の行が一致することをテストで見る (幅 20 は一番短い形も入らず、両方とも板が同じ形を切る)
  — `fix(restartable): 遷移中の板の確認の案内も confirm.FitHint で幅に合わせ、通常時と同じ文言にする`
- 変異: transitionPanel を `confirm.HintYesNo` の直置きへ戻す → `TestConfirmHintIsSameDuringTransition` が red
- tuikit と restartable の `make lint` / `make test` rc=0、`tests/scripts/test_widthenv_guarded.sh` と `test_tuikit_consumers_aligned.sh` rc=0。`go mod tidy -diff` 差分 0
- 敵対レビューは省いた: 表示の文言だけの変更で、判断ロジック・境界・状態遷移・外部 I/O が動いていないため
