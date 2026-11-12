# 1069 (perf): slack-bbs が tmux の中で同期出力を使い、本文のスクロールのたびに画面全体を外側の端末へ送り直している

起票日: 2026-11-12

## 概要

slack-bbs で本文を読んでいるとスクロールがもっさりする (ユーザーの報告 2026-11-12)。performance の監査で測ったところ、
画面のモデルの処理は重くなかった。重いのは、tmux から外側の端末 (Terminal.app) へ送る量だった。

- slack-bbs は bubbletea に同期出力 (DECSET 2026) を使わせている。tmux 3.7c は、同期出力の区間が終わるたびにペインを丸ごと描き直す。
  このため、本文が 1 行動くたびに、ほぼ画面全体を外側の端末へ送る
- 同じ現象は 2026-10-25 に glogx・treefiler で確かめ、`treefiler/termsync` (`DropSyncReport`) で止めている。
  経緯は `docs/glogx-bubbletea-v2.md` の「ちらつき低減」の項。slack-bbs はこれを使っていない

## 実測 (2026-11-12、M3 Max、tmux 3.7c、外側の端末は Terminal.app 488 で 437x104 と 382x92)

実データは `~/.cache/slack-bbs` の一番長い話題 (本文 18,655 バイト、31 話題のチャンネル)。画面の大きさは実際の client の大きさ (437x104) に合わせた。

| 測ったもの | 値 | 測り方 |
|---|---|---|
| キー 1 回の処理と描画 (`Update` + `View`) | 160x50 で 0.57ms、437x104 で 1.24ms | `go test -bench` で j/k を押し続ける。1 秒 60 回の予算 16ms に対して小さい |
| アプリがペインへ書く量 (j 1 回) | 約 4.6KB | `tmux pipe-pane` で取る。bubbletea はスクロール領域 (DECSTBM) で行をずらし、差分だけを書く |
| tmux が外側の端末へ送る量 (j 1 回) | **約 86KB** (220x60 では約 51KB) | 隔離した tmux (`-L`) に pty で client をつなぎ、キーごとに読んだバイト数 |
| 同じ量の `less` の j 1 回 | 約 7.6KB (220x60 では約 4.0KB) | 同上 |
| 同期出力の答えを捨てたとき (`DropSyncReport` と同じ filter) | 約 49KB (86KB から 43% 減) | 同上 |
| 何も起こらないキー (`w`)・本文の末尾での j | 0 バイト | 対照。送る量はスクロールが起きたときだけ出る |
| キーから tmux のペインの中身が変わるまで | slack-bbs 22ms、`less` 11ms (中央値) | `send-keys` の後に `capture-pane` が変わるまでを数える (`capture-pane` 1 回で約 5ms を含む)。差は bubbletea の 1 秒 60 回の描画の周期で大半が説明できる |

同期出力だけが原因かを、アプリを外して確かめた。437x104 のペインで、1 行ずつスクロールする bash のスクリプト (スクロール領域の高さ 2 通り × 背景色の有無) を動かし、
tmux が外側へ送る量を比べた:

- 同期出力なし: どの組でも 1 行あたり約 330〜350 バイト (tmux は外側へもスクロールで渡す)
- 各フレームを `\e[?2026h` … `\e[?2026l` で囲む: 約 31KB (約 90 倍。ペインを丸ごと描き直す)

**Terminal.app が 86KB を描く時間は測っていない** (外側の端末の描画は tmux の外で、測る手段を作っていない)。「もっさり」の体感が送る量で決まるという結び付きは見立て。
glogx と treefiler では、2026-10-25 にユーザーの依頼で同じ緩和を入れた (`docs/glogx-bubbletea-v2.md`)。
そこでは兄弟の移動 1 回で外側へ送る量が 74〜86KB から 15〜16KB に減った。slack-bbs で同じ filter を当てたときの減り方 (86KB → 49KB) より大きい。

## 原因

- bubbletea v2 は、`TERM_PROGRAM` が `Apple` を含まなければ、同期出力が使えるかを端末に問い合わせる。
  判定は `tea.go` の `shouldQuerySynchronizedOutput`。
  tmux の中は `TERM_PROGRAM=tmux` なので問い合わせ、tmux は「使える」と答える
- 外側の Terminal.app は同期出力を持たない (tmux の `client_termfeatures` に `sync` が無い)
- tmux はペインの中で同期出力の区間を受け取り、区間が終わるとペインを描き直す
- slack-bbs の起動は 2 か所 (`view.Run` と、購読の画面の `subs/picker.go` の `tea.NewProgram`)。
  どちらも `termcaps.SyncedOutput` を見ておらず、同期出力の答えを捨てていない

## 同じ形の起動 (横展開)

`git grep -n 'tea.NewProgram(' -- 'src/**/*.go' ':!**/*_test.go'` で 10 か所。`termsync` を使っているのは 2 か所 (glogx `tui.go`、treefiler `main.go`) だけ。

使っていない 8 か所:

- slack-bbs `view/view.go` (`Run`)・slack-bbs `subs/picker.go`
- mac-lan-manager `main.go`・pro-con `main.go`・restartable `internal/ui/presenter.go`・schedkeys `main.go`
- tuikit `examples/listdetail`・tuikit `examples/toast`

どれも tmux + Terminal.app で同じことが起きるはずだが、測ったのは slack-bbs だけ。

## 対応方針

1. `treefiler/termsync` を tuikit へ移す。slack-bbs を treefiler の module に依存させないため。glogx と treefiler は移した先を使う
2. slack-bbs の 2 か所の `tea.NewProgram` に、`termcaps.SyncedOutput` が false のとき `termsync.ProgramOptions` を渡す
3. 残りの 6 か所 (examples を除けば 4 か所) にも渡すかは、それぞれ画面全体が動くかを見て決める。画面がほとんど動かないものは効果が小さい
4. 直した後、上の表の「tmux が外側の端末へ送る量」を同じ測り方で測り直して、ここに書く

## 残る量 (同期出力を止めた後の約 49KB) — 見立て、未確認

止めた後も、外側へ送る量は `less` の約 6 倍残る。外側へ送った中身では、tmux はスクロール (`\e[11;99r`) を渡したうえで、ほかに 35 行を描き直していた。
bubbletea は左右を合わせた全幅の行をスクロール領域でずらすので、左の一覧も 1 行ずれ、それを描き直す分と読んでいる (確かめていない)。
本文を読む間だけ右の欄を全幅にする試作では、約 69KB (同期出力あり) だった。同期出力を止めた上で全幅にしたときは測っていない。
レイアウトを変える話なので、1〜4 の後に体感が残るときに考える。

## 関連ファイル

- `src/slack-bbs/view/view.go` (`Run`)・`src/slack-bbs/subs/picker.go` (`tea.NewProgram`)
- `src/treefiler/termsync/termsync.go` (`DropSyncReport`・`ProgramOptions`)
- `src/tuikit/termcaps/termcaps.go` (`SyncedOutput`)
- `docs/glogx-bubbletea-v2.md` (「ちらつき低減 (mode 2026 / synchronized output)」の項)

## 進捗

- [x] 監査 (performance、直接実行) で原因を測った (2026-11-12)
- [ ] 1〜4 (未着手)
