# 433 (human): pro-con の PG 用の Claude Code 設定ディレクトリで 1 度ログインする

起票日: 2026-09-24
期限: 2026-10-01

親: [415](415-design-claude-pm-worker-orchestration.md) / 出典: [431](431-feat-pro-con-pg-session-settings.md) の計測

## なぜ人が要るか

PG の起動時の token の大部分 (約 14 万文字の規約の注入) は、ユーザーの設定ディレクトリ (`~/.claude/`) から読まれる。
PG 用の設定ディレクトリ (`CLAUDE_CONFIG_DIR`) に切り替えれば外せるが、そのディレクトリでは未ログインになる (`Not logged in`)。
`/login` はブラウザでの認証が要るので、Claude からはできない。

## 手順

1. 端末で次を打ち、表示に従ってログインする (ブラウザが開く):
   `mkdir -p ~/.config/pro-con/claude-pg && CLAUDE_CONFIG_DIR=~/.config/pro-con/claude-pg claude /login`
2. ログインできたか確かめる (モデルは呼ばないので枠は使わない):
   `CLAUDE_CONFIG_DIR=~/.config/pro-con/claude-pg claude -p "/usage"` が「Current session: …」を出せば済み
3. 済んだら、この issue を `issues/epic/415/done/` へ移す (または Claude に「433 は済んだ」と伝える)

## 期待と違ったとき

- 2 で `Not logged in` のまま → 431 に「PG 用の設定ディレクトリはログインを引き継げない」と書き、別の手 (`claude setup-token` の長期トークン) を試す
- ディレクトリの場所は仮 (`~/.config/pro-con/claude-pg`)。別の場所が良ければ 431 に書く

## 進捗

- [ ] 未確認
