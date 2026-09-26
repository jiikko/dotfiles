# 517 (ux): すべての入力欄で日本語入力を見直し、送る前に確認を出す (変換の確定の Enter で送らない)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼: 「カード詳細画面で回答できるけど、日本語入力には対応している? enter を押すときに日本語入力対応しつつ、確認ダイアログが欲しい」。

続けての依頼 (同日): 「pro-con の実装にある、それぞれの入力フォームで日本語対応ができているか見直して。不備があれば修正して。
危惧していることは、変換確定したら submit されないか心配している」。**回答だけでなく全部の入力欄が対象**。

## 入力欄の一覧 (2026-09-26、`ui/model.go` の `inputKind` と `ui/answerform.go` から)

| 入力欄 | 開くキー | 編集の部品 | Enter で起きること |
|---|---|---|---|
| 新しい依頼 (`inputNew`) | `+` | `m.line` | `submit` → 送る |
| issue の補足 (`inputIssue`) | `i` で選んだ後 | `m.line` | `submit` → 送る |
| 追加オーダー (`inputOrder`) | カードで | `m.line` | `submit` → 送る (方針変更だけ y/N) |
| btw (`inputBtw`) | カードで | `m.line` | `submit` → 送る |
| 回答 1 行 (`inputAnswer`) | `r` | `m.line` | `submit` → 送る |
| 回答フォームの自由記述 (493) | `r` (選択肢つき) | フォームの自分の欄 | `submitForm` → 送る |
| 終了 (`inputQuit`) | `q` | `m.line` | 「quit」と打ったときだけ閉じる |

見直す観点 (欄ごとに):
- 変換中の文字の位置 (カーソルを欄のキャレットに置いているか。`view.go` の `caret` / フォームの `caretX`)
- 変換を確定する Enter が送信にならないか (下の「確かめ方」)
- 全角の文字の幅でキャレットと折り返しがずれないか (`ansi.StringWidth` で数えているか)
- 貼り付け (bracketed paste) で改行が送信にならないか

## 今どうなっているか (2026-09-26 にコードで確かめた)

- 日本語入力: 1 行の回答欄 (`r`。`ui/view.go` の `caret`) も、選択肢の回答フォーム (493。`ui/answerform.go` の `caretX` / `caretY`) も、
  変換中の文字が出る位置に端末のカーソルを置いている (IME の変換中の文字が入力欄の外に出ないように)
- 変換を確定する Enter は、ふつう端末 (IME) が受け、アプリには届かない。**ユーザーの端末 (tmux の中) で確かめたことは無い**
- 送る前の確認は無い: 1 行の欄は Enter で `submit` → `apply`、フォームは Enter で `submitForm`。確認 (y/N) が出るのは追加オーダーの「方針変更」だけ (`submit` の `askConfirm`)

## 期待する動作

- 上の表の送る欄 (終了以外) を Enter で送ると、送る前に確認を出す: 送る中身 (選んだ選択肢・書いた文) を見せて、y で送る / それ以外で入力に戻る
  (取り消しで書いた文を消さない。方針変更の確認は取り消すと入力を消しているので、回答では消さない)
- 日本語入力の確定の Enter で確認が出ない (端末が Enter を渡さない限り)。万一渡る端末でも、確認で止まるので送られない
- 確認は 1 つの仕組みで全部の欄に付ける (欄ごとに別の確認を作らない)
- 見直しで見つけた不備 (キャレット・幅・貼り付け) は同じカードで直す

## 日本語入力の処理が分かれている所 (同日のユーザーの指示「分散しているなら dotfiles/src の下に lib として切り出して」)

- 編集は既に共通: `src/tuikit/lineedit` (pro-con の `m.line` とフォームの欄が使う)。貼り付けの改行・タブは空白に置き換える (`Line.Insert`) ので、貼り付けでは送られない
- **分かれているのは、変換中の文字を出す位置 (キャレット) を画面の座標に直して端末のカーソルに置く所**: `pro-con/ui/view.go` の `caret` /
  `pro-con/ui/answerform.go` の `caretX`・`caretY` と `tea.NewCursor` / `schedkeys/layout.go` の `frame.render`。
  どれも「キャレットの前の文字の表示幅 + 欄の左端」を別々に数え、`tea.CursorBar` を別々に付けている
- → **`src/tuikit` に切り出す** (例: `lineedit` に「キャレットまでの表示幅」、描く側に「欄の位置からカーソルを作る」1 つの関数)。3 か所をそれに寄せる。
  全角の幅の数え方もそこに 1 つにする (`ansi.StringWidth` か `tuikit/termwidth` のどちらかに揃える)
- 1 回に何文字もまとめて届くキー (日本語の確定は「日本語」がまとめて 1 つのキーで届く): `lineedit.Line.Key` が text をそのまま差し込むので pro-con では問題ない。
  glogx (`tui.go`) は入力欄を持たないので、まとまった文字を 1 文字ずつのキーに分けている (別の目的なので寄せない)

## 確かめ方

- 機械で確かめられる所: 各欄で、全角の文字を打った後のキャレットの位置・幅 / 貼り付けに改行が入っても送らない / Enter で確認が出る (テスト)
- 🚨 **変換を確定する Enter がアプリに届くかは端末と IME しだいで、機械では IME の確定を作れない**。tmux は変換中の文字を見ない
  (変換は tmux の外の端末で起き、確定した文字だけが tmux に届く) ので、届くとすれば端末が確定のあとに Enter も送る形。
  確認を付けておけば、届いても送られずに止まる。最後に人が端末で 1 回確かめる (受け入れ条件の最後)

## 対応方針

- 既にある `askConfirm` と `confirm.IsYesStrict` を使う (確認の仕組みを 2 つ作らない)
- 確認の見た目 (中身の見せ方) は、見本を出して人に選んでもらう

## 受け入れ条件

- [x] 上の表の送る欄すべてで、Enter → 確認 → y で送られ、それ以外で入力に戻り、書いた中身が残る (`ui/sendconfirm_test.go` の `TestEverySendFieldConfirmsBeforeSending`)
- [x] 各欄で全角の文字のキャレット・幅がずれず、貼り付けの改行で送られない (テスト。`ui/ime_test.go` の `TestCaretStaysAfterLastCharForLongText` / `TestPasteWithNewlineDoesNotSend`)
- [x] キャレットを画面の座標に直す処理が `src/tuikit` の 1 か所にあり、pro-con の 2 か所と schedkeys がそれを使う (`tuikit/caret.At` と `lineedit.Line.Window`)
- [x] 確認に送る中身が出る
- [ ] 人が確かめる: ユーザーの端末 (tmux の中) で、新しい依頼と回答の欄で日本語を変換して確定する Enter で、確認も送信も起きない (機械では IME の確定を作れないため)

## 関連ファイル

- `src/tuikit/lineedit/lineedit.go` / `src/schedkeys/layout.go` (`frame.render`)
- `src/pro-con/ui/model.go` (`submit` / `askConfirm`) / `src/pro-con/ui/answerform.go` (`submitForm`) / `src/pro-con/ui/view.go` (`caret`)

## 進捗

### 2026-09-26 カード C-077

- 人が見本 3 案から選んだ: **B 中央の枠** (回答フォームと同じオレンジの丸い枠に送る中身を折り返して全部出す) / 確認中の **y と enter で送る** (`confirm.IsYesStrict`)。
  PG は「enter でも送ると、端末が確定の enter を渡したとき 2 回目の enter で送られる」ので y だけを推したが、人は enter も採った
- 仕組み: 既存の `modeConfirm` / `pending` に `send *sendConfirm` を足しただけ (`ui/sendconfirm.go`)。`send` が在れば中央の枠を出し、
  取り消すと元の画面 (入力欄 / フォーム) へ書いた中身のまま戻る。無ければ従来の破壊的な操作の y/N (取り消すとボードへ戻り入力を消す)。
  方針変更の確認もこれに寄せた (枠に「PG を止めて、指示を差し替えて再開します」を黄色で出す。取り消しで入力を消さなくなった)
- 見直しで直した不備:
  - **1 行の入力欄は長い文でキャレットが欄の外に出ていた** (欄を窓にせず、キャレットの桁を `width-1` に寄せるだけ。文字は `…` で切られ、
    変換中の文字が最後の文字とずれた所に出る)。フォームの欄と同じく、キャレットが見えるよう前を切る (`lineedit.Line.Window`)
  - 確認の最中の ctrl+c が書いた中身を捨てて終了の入力欄へ移らないようにした (入力中と同じく断る。`holdsDraft`)
- `src/tuikit/caret` (`At(x, y, 幅, 高さ)`: 棒のカーソル・幅の外は最終列へ・画面の外の行は nil) を新設し、pro-con の `caret` / `formCaret` と
  schedkeys の `frame.render` が使う。**schedkeys のカーソルは棒の形になった** (前は既定のブロック)。幅は termwidth (= ansi.StringWidth) で数える
- 敵対的レビューで直したもの: e2e の通し (`e2ecmd.go` / `tests/pro-con/e2e_screen_killed.sh`) が enter の後に y を送っていなかった /
  テストが下に残る入力欄・案内の行で通っていた (枠だけを描いて見る `confirmBox` にした) / btw の宛先と本文をどのテストも固定していなかった /
  空の方針変更に「PG を止める」を出していた
- 承知で残したもの: 確認を取り消したキーの文字は入力欄に入らない (IME の確定語句ごと enter の後に続けると、その語句は捨てる。「他のキーで戻る」を人が選んだ形のまま) /
  確認の最中に宛先のカードが消える・状態が変わると、y が backend に断られて書いた中身を捨てる (入力中に起きるのと同じ既存の穴。確認を挟んだぶん待つ時間が延びる)
- 残り: 受け入れ条件の最後 (人が端末で日本語を変換して確定する enter で確認も送信も起きないか) は人が確かめる
