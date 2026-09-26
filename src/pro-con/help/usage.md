# 使い方 (pro-con help usage)

引数の全部は各コマンドの usage (`pro-con card` を引数なしで・`pro-con log --help` など)。ここは「どの場面で何を使うか」だけ。

## 依頼を出す

- 新しい作業: 画面の `n` (issue から選ぶなら `i`) か `pro-con card add --title <題名> --request <依頼の原文> --repo <repo>` (--repo は省いてよい)。
  依頼の列に入り、PM が issue に分けて着手待ちの列に積む。add は適用を待ってカード ID を出す (待てなければ依頼 ID を出して rc=3)
- 動いているカードへの足し: 画面の `+` か `pro-con card order <カード> <本文>`。3 つを使い分ける:
  - **追記** (既定): PG の turn の区切りで届く。PG は止まらない
  - **方針変更** (`--redirect`): PG を止めて、指示を差し替えて同じ session を再開する (worktree の途中の変更は残る)
  - **別件**: 元のカードとは別の作業。`card add` で新しい依頼にする (完了のカードにも order は出せない)
- 並びを変える: 画面の `K` / `J` か `pro-con card move <カード> up`。レーンの中は上ほど先に起動する
- 状況を聞くだけ: 画面の `w` (btw)。PG に届けず止めず、答えはカードの履歴に出る

## 質問に答える

- 質問待ちのレーンのうち **人の番** のものだけが人の答えを待っている (PG の質問はまず PM が受ける)
- 画面の `r`。選択肢つきの質問 (`card ask --json`) なら回答フォームが開く
- 外からは `pro-con card answer <カード> <回答>`。選択肢つきの質問には `1. <見出し>: <選んだ名前>` の行を問いごとに並べて答える
  (フォームと同じ形)。回答で PG の同じ session が再開する
- `?権限` (権限の確認・AskUserQuestion で止まった PG) は回答を受けない。人が画面の `a` で attach して答える
- 答えの前に `pro-con card show <カード>` で質問の全文と履歴を読む

## attach から戻る

- 画面の `a` で PG の session に attach する (`claude attach`)。attach の間に打った文は、戻るとカードの履歴に残る
- tmux の中: attach は pro-con の画面の上の窓 (tmux の popup) で開く。窓の枠の見出しにあるキーで閉じて pro-con に戻る: tmux の prefix に続けて `d` (普段の detach と同じ手。外のセッションは detach されない) か Ctrl+Z。prefix のキーそのものを Claude Code に送るには prefix を 2 回
- tmux の外: attach の前に戻り方の案内が出る (enter / y で進む)。Ctrl+Z で pro-con に戻る。`←` は Claude Code の一覧へ行くだけで戻らない
- 戻れなくなったら、別の端末で `pro-con attach --leave`。pro-con の画面から開いた attach の接続だけを終わらせる (画面を複数開いていれば、その全部の attach を終わらせる)
- どの戻り方でも PG の session は動き続ける (終わるのは attach の接続だけ)
- レビュー待ちのカードの PG は、turn を終えたら dispatcher が止める (issue 536)。止めた後は attach できない。話すなら `+` で追加オーダーを出す (同じ session を続きから再開して届ける)。差し戻し (`card rework`) も同じ session を続きから再開する

## 待つ・見る

- `pro-con card list` / `pro-con card show <カード>` / `pro-con card log <カード>` (PG の活動。--follow で追う)
- `pro-con card wait <カード> --until review` (その列に来るまで待つ。既定 10 分で時間切れは rc=1)
- `pro-con screen` で人の画面に今出ているものを読む (読むだけ)

## 画面の開き方

| 開き方 | 読み書き | dispatcher を起こす | 閉じたとき |
|---|---|---|---|
| `pro-con` (持ち主) | する | する | 最後の持ち主なら dispatcher と PG を止める |
| `pro-con --join [--as <名前>]` | する | しない | 何も止めない |
| `pro-con --view` | しない (見るだけ) | しない | 何も止めない |

- 画面を閉じるのは `Q` → `quit` と打って enter (q / esc では閉じない)
- 止めるだけなら `pro-con dispatcher --stop` (人が止めた印が残り、画面は起こし直さない。画面の `c` か手で `pro-con dispatcher` を起動すると外れる)

## 消す・片付ける

- 削除: 画面の `d` か `pro-con card delete <カード>`。依頼の列はすぐ消え、ほかは PG の session を止めてから消える。
  worktree とブランチは残る
- 片付け: 画面の `x` で完了のレーンを書庫へ移す (完了から 24 時間で自動でも移る。1 週間で書庫からも消える)
- PG の worktree: `pro-con worktree clean` で一覧を見て、`--yes` で消す (取り込み済みで中に誰も居ないものだけ)。`--remote` を付けると origin に残った PG のブランチも並べて消す (完了したカードで取り込み済みのものだけ)
- ディスクの使用量は `pro-con du`
