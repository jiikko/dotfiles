# 527 (ux): attach から pro-con へ戻る手段を分かりやすくする

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザー (2026-09-26): 作業中のカードに `a` で attach したら Claude Code の画面になり、pro-con への戻り方が分からなかった。
「attach をしてから戻る手段をもっとわかりやすくしたい」。

その場では、外から attach の接続 (`claude attach 58b1d3fb`、pro-con の画面の子) に SIGTERM を送って戻した。PG の session (`--bg` の本体) は動いたままだった。

## 今どうなっているか (2026-09-26 に確かめた)

- `a` は `claude attach <session>` を `tea.ExecProcess` で起こす (`live/live.go`・`ui/switchfade.go` の `execOnTerminal`)。attach の間、画面は Claude Code のもの
- `claude attach --help` の文: 「← returns to agent view, Ctrl+Z drops back to your shell. The session keeps running either way.」。
  pro-con へ戻るのは **Ctrl+Z**。`←` は Claude Code の agent の一覧へ行くだけで pro-con には戻らない
- pro-con の側では、attach の前にも間にも戻り方を出していない。README の attach の項にも戻り方が無い
- 🚨 Ctrl+Z で戻ったとき pro-con が正しく描き直すかは、518 (画面が止まり fg で戻らない) と関係するかもしれない (未確認)

## 続けての依頼 (同日): pro-con が用意した「戻る」を画面に出す

ユーザー: 「画面にガイド・ヘルプを出せない? ctrl+z とかではなく、pro-con が用意した戻る選択肢を画面に表示できないの?」。
今は `tea.ExecProcess` で端末を丸ごと `claude attach` に渡すので、attach の間 pro-con は描けず、キーも受け取れない。渡し方を変えれば出せる:

| 案 | 見え方 | 戻り方 | 重さ |
|---|---|---|---|
| **A. tmux の popup で開く (推す)** | pro-con の画面の上に枠つきの窓 (`tmux display-popup -E -T '<見出し>'`) で Claude Code。枠の見出しに「pro-con に戻る: <キー>」。窓の外に pro-con が見えたまま | pro-con が決めたキーで窓を閉じる (attach の接続だけが終わり、PG の session は動いたまま) | 軽い。tmux の中だけ |
| B. 中継して下に案内の行 | pro-con が端末と Claude Code の間に入り、Claude Code の画面の下に pro-con の案内の 1 行を常に出す | pro-con が拾うキー (例: `Ctrl+]`) で attach の接続を切る | 重い (端末の表示を組み立て直す部品が要る) |
| C. tmux の pane を分ける | 左右に pro-con と Claude Code | pro-con の側のキー / pane を閉じる | 軽い。tmux の中だけ・狭くなる |

- **決定 (2026-09-26、ユーザー「A でお願いします」): A (tmux の popup) で作る** (ユーザーの pro-con はいつも tmux の中)。B・C は作らない。tmux の外では、下の「アイディア」の案内 (attach の前の案内・Ctrl+Z) に戻る
- 🚨 popup の中で `claude attach` を起こすと、pro-con の `ExecProcess` を通らない (端末を渡さない) ので、518 の「前面が外れる」経路も避けられる見込み (未確認)
- 🚨 `tmux display-popup` / `bind-key` を本番の tmux サーバで試さない。確かめは隔離した tmux の `-L` サーバで。popup を閉じるキーを tmux に bind するなら、attach の間だけにして戻したら外す
- attach から戻ったときに人間の発言をカードの履歴へ残す処理 (README の attach の項) は、popup でも同じく動くこと

## アイディア (作り方は PG が決めてよい)

- attach の直前に 1 画面の案内を出す: 「戻るには Ctrl+Z (← は Claude Code の一覧へ行くだけ)。PG は動き続ける」。Enter で attach へ進む (毎回出すか、初回だけかは見本で決める)
- attach の間も見える所に出す: tmux の中なら、attach の間だけ status 行か pane の枠の見出しに「Ctrl+Z で pro-con へ戻る」を出す (tmux の外なら端末のタイトル)。戻ったら元に戻す
- 戻れなくなったときの逃げ道: `pro-con attach --leave` のような、外から attach の接続だけを終わらせる口 (その場で手でやったことを道具にする。PG の session には触らない)
- `?` のヘルプ・README・`pro-con help usage` (510) に戻り方を書く

## 受け入れ条件

- [ ] tmux の中では、attach が pro-con の上の窓 (A) で開き、窓に pro-con が用意した戻る操作が出て、それで pro-con に戻る (見た目は人が見本から選ぶ)
- [ ] tmux の外では、attach する前か間に、戻る操作 (Ctrl+Z) が画面に出る
- [ ] Ctrl+Z で pro-con に戻り、画面が描き直されてキーが効く (隔離した tmux の `-L` サーバで確かめる。本番の tmux サーバでやらない)
- [ ] 戻れなくなったときに、外から attach の接続だけを終わらせる手段があり、PG の session は動き続ける
- [ ] ヘルプ・README・`pro-con help` に戻り方がある

## 関連ファイル

- `src/pro-con/ui/model.go` (`a`) / `src/pro-con/ui/switchfade.go` (`execOnTerminal`) / `src/pro-con/live/live.go` (`claude attach` を組む所) / 518

## 順番の見積もり (PM, 2026-09-26。C-082 を C-078 の後に積んだ)

- 触る場所: `ui/switchfade.go` の `execOnTerminal` (attach の前後)、`ui/model.go` の `a`、`live/live.go`、ヘルプと README
- 変える判断: attach から戻ったとき (Ctrl+Z) に画面をどう取り戻すか
- 順番の理由: 518 (C-078) が同じ `execOnTerminal` の前後と、止まった画面が fg で戻る経路を直している (レビュー中)。こちらの受け入れ条件「Ctrl+Z で戻って描き直される」は
  518 の直し方の上でしか確かめられないので、518 の後にする (依頼の原文でも指定)。519 (C-079) も 518 の後で、終了の経路を触るが attach とは別の所なので、C-079 との順番は付けていない

## 進捗

(まだ無い)
