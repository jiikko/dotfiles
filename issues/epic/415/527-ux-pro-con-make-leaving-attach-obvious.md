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

## アイディア (作り方は PG が決めてよい)

- attach の直前に 1 画面の案内を出す: 「戻るには Ctrl+Z (← は Claude Code の一覧へ行くだけ)。PG は動き続ける」。Enter で attach へ進む (毎回出すか、初回だけかは見本で決める)
- attach の間も見える所に出す: tmux の中なら、attach の間だけ status 行か pane の枠の見出しに「Ctrl+Z で pro-con へ戻る」を出す (tmux の外なら端末のタイトル)。戻ったら元に戻す
- 戻れなくなったときの逃げ道: `pro-con attach --leave` のような、外から attach の接続だけを終わらせる口 (その場で手でやったことを道具にする。PG の session には触らない)
- `?` のヘルプ・README・`pro-con help usage` (510) に戻り方を書く

## 受け入れ条件

- [ ] attach する前か間に、戻る操作 (Ctrl+Z) が画面に出る
- [ ] Ctrl+Z で pro-con に戻り、画面が描き直されてキーが効く (隔離した tmux の `-L` サーバで確かめる。本番の tmux サーバでやらない)
- [ ] 戻れなくなったときに、外から attach の接続だけを終わらせる手段があり、PG の session は動き続ける
- [ ] ヘルプ・README・`pro-con help` に戻り方がある

## 関連ファイル

- `src/pro-con/ui/model.go` (`a`) / `src/pro-con/ui/switchfade.go` (`execOnTerminal`) / `src/pro-con/live/live.go` (`claude attach` を組む所) / 518

## 進捗

(まだ無い)
