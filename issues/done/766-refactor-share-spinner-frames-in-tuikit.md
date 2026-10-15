# 766 (refactor): spinner の字形と「時刻からコマを選ぶ関数」を tuikit に寄せる (bubbles は取り込まない)

起票日: 2026-10-15

## 概要

ユーザー依頼 (2026-10-15): 「bubbles は他のツールにも適用できることがあったら取り込んで欲しい」。

747 で lanmac を glogx の全画面の板として埋め込むと決め、glogx が lipgloss v2 / bubbles v2 に依存することになる
(747 の「決定 (2026-10-15 ユーザー回答)」節)。依存が入るなら、ほかの TUI が自前で書いている同じ働きの部品を bubbles へ寄せられないかを調べ、
寄せられるものは寄せる。bubbles は charm が出している bubbletea 用の出来合いの部品集 (spinner / progress / help + key / textinput / list / viewport / table 等)。

## 方針の変更 (2026-10-15 ユーザー回答)

- 747 の lanmac も bubbles を使わないことになった (747 の「決定 (2026-10-15)」)。この issue の前提「glogx に bubbles が入るなら、ほかの TUI も寄せる」は崩れた
- 下の調査で、bubbles に寄せて得をする部品はほぼ無いと分かった (spinner は「時刻からコマを選ぶ」性質を捨てる・help は `q` を黙って落とす・入力欄は `lineedit` が正本・
  進捗バーは自前で描いている画面が無い)。**残る価値は spinner の字形の重複 (4 か所) を tuikit に寄せること**。この issue はそこに絞る:
  tuikit に字形と「時刻からコマを選ぶ関数」を置き、glogx・treefiler・restartable・pro-con と、新しく作る lanmac がそれを使う (各画面の tick の回し方はそのまま)
- restartable は tuikit を擬似バージョンで固定しているので、tuikit の変更は restartable の go.mod の版を上げるまで届かない (`src/tuikit/CLAUDE.md`)

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

- [x] ~~747 の実装で glogx に bubbles が入る~~ → 不要になった (747 は bubbles を使わない。spinner の共有は 747 に依存しないので先に着手した)
- [x] 候補の判断: spinner だけ寄せる。help・textinput・progress・list 等は寄せない (理由は「方針の変更」節と候補の表)。キーの案内の数え上げは寄せないと決めたので不要
- [x] 1 段目 (2026-10-15): `tuikit/spinner` (`Frame(i)` / `At(時刻, 1 コマの長さ)` / `Len`。状態も tea.Cmd も持たない) を足し、glogx・treefiler・pro-con が使う。
  入口は tuikit の README の表・CLAUDE.md の地図・`docs/glogx-ui-guide.md` §9・`src/PACKAGES.md`
  - commit: 「refactor(tuikit): spinner の字形とコマの選び方を tuikit/spinner に置き…」/「…字形を文字列で固定し、At の時刻の範囲を書く」/「refactor(glogx): RenderCached の引数名…」
  - 変異 (`mutate-verify`。全部想定どおり red): 負の i の回しを外す → TestFrameWraps / At の単位を UnixMilli と混ぜる → TestAtSelectsByTime /
    step≤0 の guard を外す → TestAtNonPositiveStep / コマの入れ替え → TestFramesMatchLiteral
  - 敵対的レビュー (opus・read-only) 2 周: 1 周目 P2 テストの期待値を frames 自身から引いていて入れ替え・差し替えが緑 → 文字列リテラルで固定 /
    P3 At は UnixNano なので 1678〜2262 年の外ではコマに意味が無い → doc に書いた (本番は time.Now で届かない) / P3 glogx の引数 `spinner` が package を隠す → glyph に改名 /
    §9 が未対応の restartable を挙げていた → 外した / P3 負の時刻・frame で旧式は panic、新式は回す → 改善側なので受容。
    2 周目 P1・P2 なし、P3 RenderCached の引数も隠していた → 改名 (他ファイルの `spinner string` は package を import していないので隠しにならない)。
    2 周目の変異 8 本は 7 本 red、緑の 1 本 (At の `% Len` を外す) は Frame が回すので等価
  - make test rc=0 (rebase 後、push する木で)
- [x] 2 段目 (2026-10-15): restartable の tuikit の固定版を 1 段目を push した版 (`v0.0.0-20261015124436-ffd6e2aa8a68`) へ上げ、`internal/ui/view.go` の `spinnerFrames` と負の frame の回しを
  `spinner.Frame` に寄せた。§9 の「使っている所」に restartable を足した。`make -C src/restartable lint test` rc=0、`tests/scripts/test_tuikit_consumers_aligned.sh` は 7 module が揃って緑
  (x/ansi は v0.11.7 のまま。版上げは 747 の段 2 で dotfiles-16 が行う)。版の上げと機械的な置き換えだけなので、この段の敵対的レビューは省いた (挙動の変更なし。負の frame の回しは Frame のテスト TestFrameWraps が持つ)
- 自前の点字の列は src/ の production コードから無くなった (`grep -rn '"⠙"' src --include='*.go'` は tuikit/spinner の 1 か所だけ)

## 反証レビュー

- 初版 (2026-10-15・read-only のサブエージェント 1 体): P2 spinner の時間の進め方が 4 か所で違う・bubbles の spinner は状態機械で「時刻で選ぶ」性質を捨てる →
  採って「現状」と候補の表に書いた / P2「tuikit は描画のフレームワークに依存しない」は不正確 → tuikit の go.mod は bubbletea を require しているが、import するのは
  `caret` だけ (anim / lineedit / foreground も import しているというレビューの主張は grep で誤りと確認)。論点を「新しい依存を足すか」に立て直した /
  P3 caret の使い方の粗さ・進捗の表示の未確認・ratelimit が bubbletea を使わない・help が「…」すら出さずに落とすことがある → 採って書いた。
  反証できなかった: restartable の擬似バージョン固定・`docs/glogx-bubbletea-v2.md` の引用・各 module の x/ansi が v0.11.7 でそろっていること
- [ ] 候補ごとの判断
- [ ] 取り込み
