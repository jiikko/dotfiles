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
  - 5 か所目の定義は無い (同じ字形は glogx の凡例 `options.go` と `tools/width-probe` に文字列として出るだけ)
  - **時間の進め方が 4 か所で違う**: glogx は `m.frame` の整数、treefiler は `now.UnixMilli()/80`、pro-con は `now()/spinInterval` (どちらも時刻から
    コマを選ぶ純関数。描き直しの回数に依らず同じ時刻なら同じコマ)、restartable は frame を引数で受けて自前の tick で回す
- **入力欄**: glogx / pro-con / schedkeys / treefiler が tuikit の `lineedit` を使う (`docs/glogx-ui-guide.md` §7 が正本)。端末のカーソルを置く `caret` は
  glogx (`tui.go` / `issues_view.go`)・pro-con (`answerform.go` / `view.go`)・schedkeys (`layout.go`) が入力欄で使い、treefiler は `main.go` の 1 か所だけ
- **キーの案内 (hint の行)**: 各画面が手で組んでいる。数は未確認 (「案内」「hint」で grep すると glogx・pro-con でそれぞれ 20 ファイル当たるが、無関係の語も混ざる粗い数え)
- **進捗の表示**: 進捗バーを自前で描いている画面は見つかっていない。近いものは**使用率のメーター** 2 つ: pro-con の設定のディスク使用率の帯
  (`ui/settings.go`)、ratelimit の `bar()` (`usage/render.go`。10 セル固定・色の閾値 75 / 90%)。どちらも幅と閾値を固定した計器で、bubbles の
  `progress` (バネの演出・グラデーション) とは用途が違う
- **対象の範囲**: bubbletea を使う TUI (glogx / pro-con / schedkeys / treefiler / restartable)。**ratelimit は bubbletea を使っていない** (go.mod に無い) ので対象外

## 候補と判断の観点

| 部品 | 寄せる候補 | 寄せる利点 | 気を付けること |
|---|---|---|---|
| spinner | 上の 4 か所 | 字形と周期の重複が消える | 寄せ先は「tuikit に字形 (と時刻からコマを選ぶ関数) だけ置く」か「bubbles の `spinner.Model` に寄せる」か。論点は bubbletea への依存の有無ではなく (tuikit も `caret` のために bubbletea を require している)、**lipgloss / bubbles という新しい依存を各 module に足すか**。bubbles の spinner は `TickMsg` を受けるたびに 1 コマ進める状態機械で、treefiler・pro-con の「時刻からコマを選ぶ」性質 (描き直しの回数に依らない) を捨てることになる。各画面の「回すものがある間だけ tick する」規律も bubbles の `Tick` で保てるかを見る。restartable は tuikit を擬似バージョンで固定している (`src/tuikit/CLAUDE.md` の「消費者」) ので、bubbles を入れると `go install` の依存が増える。**現実的なのは字形の共有 (tuikit) で、`spinner.Model` への置き換えは不利が大きい見込み** |
| help + key | 各画面の案内の行 | キーの定義と案内が 1 か所になる | 🚨 bubbles の `help` は幅を超えた項目から先を項目単位で全部落とし、` …` が収まるときだけ付ける (収まらなければ何も付けずに無言で落とす。`help/help.go` の `shouldAddItem`、bubbles v2.2.1)。ガイド §5「抜ける手段 (`q`) は必ず残す」と衝突する (747 の見本で、幅 120 で末尾の `q 終了` が切れたのを実測)。`q` を先頭に置く・出すキーを絞るなどの規律が要る。glogx は入力中に案内を差し替える (§5) |
| textinput | `lineedit` の置き換え | — | ガイド §7 は `lineedit` + `caret` が正本。`textinput` の編集キーが §7 と同じかは未確認。原則は置き換えない (747 でも本体の入力欄は `lineedit` を推した) |
| progress | 無し (上の「進捗の表示」) | — | 使用率のメーター (pro-con のディスク・ratelimit の `bar()`) は用途が違うので寄せない見込み。ratelimit はそもそも対象外 |
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
- [ ] 候補の洗い出し (spinner の 4 か所・入力欄・進捗の表示は済み。キーの案内の対象を数える)

## 反証レビュー

- 初版 (2026-10-15・read-only のサブエージェント 1 体): P2 spinner の時間の進め方が 4 か所で違う・bubbles の spinner は状態機械で「時刻で選ぶ」性質を捨てる →
  採って「現状」と候補の表に書いた / P2「tuikit は描画のフレームワークに依存しない」は不正確 → tuikit の go.mod は bubbletea を require しているが、import するのは
  `caret` だけ (anim / lineedit / foreground も import しているというレビューの主張は grep で誤りと確認)。論点を「新しい依存を足すか」に立て直した /
  P3 caret の使い方の粗さ・進捗の表示の未確認・ratelimit が bubbletea を使わない・help が「…」すら出さずに落とすことがある → 採って書いた。
  反証できなかった: restartable の擬似バージョン固定・`docs/glogx-bubbletea-v2.md` の引用・各 module の x/ansi が v0.11.7 でそろっていること
- [ ] 候補ごとの判断
- [ ] 取り込み
