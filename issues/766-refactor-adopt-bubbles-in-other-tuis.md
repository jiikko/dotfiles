# 766 (refactor): bubbles (charm の画面部品) を lanmac 以外の TUI にも取り込めるところを洗い出して取り込む

起票日: 2026-10-15

## 概要

ユーザー依頼 (2026-10-15): 「bubbles は他のツールにも適用できることがあったら取り込んで欲しい」。

747 で lanmac を glogx の全画面の板として埋め込むと決め、glogx が lipgloss v2 / bubbles v2 に依存することになる
(747 の「決定 (2026-10-15 ユーザー回答)」節)。依存が入るなら、ほかの TUI が自前で書いている同じ働きの部品を bubbles へ寄せられないかを調べ、
寄せられるものは寄せる。bubbles は charm が出している bubbletea 用の出来合いの部品集 (spinner / progress / help + key / textinput / list / viewport / table 等)。

## 現状 (2026-10-15・grep で読んだだけ)

- **spinner**: 同じ字形の並び (`⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏`) が 4 か所に重複している
  - `src/glogx/render.go` の `spinnerFrames`
  - `src/treefiler/filer/model.go` の `spinnerFrames` (`spinnerFrame(now)` で時刻から 1 コマを選ぶ)
  - `src/restartable/internal/ui/view.go` の `spinnerFrames`
  - `src/pro-con/ui/spinner.go` の `spinFrames` (回すカードがある間だけ 100ms で描き直し、無くなったら止める)
- **入力欄**: glogx / pro-con / schedkeys / treefiler が tuikit の `lineedit` + `caret` を使う (`docs/glogx-ui-guide.md` §7 が正本)
- **キーの案内 (hint の行)**: 各画面が手で組んでいる。数は未確認 (「案内」「hint」で grep すると glogx・pro-con でそれぞれ 20 ファイル当たるが、無関係の語も混ざる粗い数え)
- **進捗の表示**: 未確認 (`progress` で grep すると無関係の語にも当たるので数えていない)

## 候補と判断の観点

| 部品 | 寄せる候補 | 寄せる利点 | 気を付けること |
|---|---|---|---|
| spinner | 上の 4 か所 | 字形と周期の重複が消える | 寄せ先を tuikit (描画のフレームワークに依存しない部品集) にするか bubbles (bubbletea に依存) にするか。restartable は tuikit を擬似バージョンで固定している (`src/tuikit/CLAUDE.md` の「消費者」) ので、bubbles を入れると `go install` の依存が増える。各画面の「回すものがある間だけ tick する」規律を bubbles の `spinner.Tick` で保てるか |
| help + key | 各画面の案内の行 | キーの定義と案内が 1 か所になる | 🚨 bubbles の `help` は幅が足りないと末尾から「…」で切る。ガイド §5「抜ける手段 (`q`) は必ず残す」と衝突する (747 の見本で、幅 120 で末尾の `q 終了` が切れたのを実測)。`q` を先頭に置く・出すキーを絞るなどの規律が要る。glogx は入力中に案内を差し替える (§5) |
| textinput | `lineedit` の置き換え | — | ガイド §7 は `lineedit` + `caret` が正本。`textinput` の編集キーが §7 と同じかは未確認。原則は置き換えない (747 でも本体の入力欄は `lineedit` を推した) |
| progress | 進捗を出している画面 | バネの演出・グラデーション | 対象の洗い出しから |
| list / viewport / table | 一覧・pager | — | `docs/glogx-bubbletea-v2.md` の「採用していない v2 機能」: glogx の一覧・pager・overlay は要望起点の独自挙動が固まっていて、置き換えは複雑性の移動にしかならない。原則は対象外 |

## 前提・制約

- charm の依存の版は module 間でそろえる (`tests/scripts/test_tuikit_consumers_aligned.sh`)。bubbles を入れた module では x/ansi が上がる (747 の見本で v0.11.7 → v0.11.8)
- 寄せるかは行数ではなく、重複の除去・語彙の 1 本化になるかで決める (`_claude/rules/verify-design-intent-before-refactor.md`)。見た目と時間の挙動を変えない置き換えは、挙動を変えない commit として単独で入れる
- 747 の実装 (glogx に bubbles が入る) が先。版合わせはそこでまとめてやるので、この issue はその後に着手する

## 対応方針

1. 候補ごとに「寄せる / 寄せない (理由)」を決める。寄せない理由はコードの直近にも残す (`_claude/rules/pending-issue-rationale-in-code.md`)
2. 寄せるものは画面 (module) ごとに 1 commit。ガイド (`docs/glogx-ui-guide.md` §9 の部品の地図) を同じ変更で直す

## 進捗

- [ ] 747 の実装で glogx に bubbles が入る
- [ ] 候補の洗い出し (spinner の 4 か所は済み。キーの案内・進捗の表示の対象を数える)
- [ ] 候補ごとの判断
- [ ] 取り込み
