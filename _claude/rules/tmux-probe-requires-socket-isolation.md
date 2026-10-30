# 破壊的 tmux コマンドは、隔離を実証してから打つ（$TMUX は TMUX_TMPDIR に優先する）

> **発動点は「破壊的 tmux コマンド（kill-server / kill-session / pkill tmux）を打つ前」**。
> 計測 probe に限らず、掃除・再現実験・デモ・スクリプトの動作確認でも同じ形になる。
> tmux ペイン内では `$TMUX` があるため、素の `tmux` は常に本番サーバを向く。

## ルール

- **破壊的 tmux コマンドの前に、隔離が効いていることを実証する**: `tmux -L <name> ls` が本番セッションを**返さない**ことを見てから打つ。これが本命の規律（危険なのは「隔離を忘れる」ではなく「隔離したつもりが成立している」ケース）
- **成功メッセージは隔離の証拠にならない**。`new-session` が成功しても、それは本番サーバにセッションが**追加**された場合と同じ出力
- **`$TMUX` は `TMUX_TMPDIR` より優先される**。tmux ペイン内（Claude セッションは大抵ペイン内で動く）では、`-L`/`-S` を付けない限りクライアントは `$TMUX` の socket = 本番サーバに接続する（2026-07-30 実測: `d=$(mktemp -d); TMUX_TMPDIR=$d tmux ls` が本番セッション一覧を返し、`$d` に socket は作られない）。つまり **`$TMUX` が生きている限り `TMUX_TMPDIR` だけの隔離は不十分**。`unset TMUX TMUX_PANE` 済みなら `TMUX_TMPDIR` に倒す方式は成立する（tests/tmux/test_fork_scratch.sh 冒頭がその方式の正本）
- **`-f /dev/null` は新サーバを立てない**。設定ファイル指定はサーバ**起動時**にしか効かず、既存サーバへの接続では黙って無視される。「素の設定の新サーバが立つ」という読みは誤り
- **破壊的な後片付けを依頼の外から自発で足さない**。共有リソース（tmux サーバ）はユーザーの領分（[`no-unauthorized-branch-switch.md`](no-unauthorized-branch-switch.md) と同ファミリー）
- **破壊的操作に `2>/dev/null` を付けない**（失敗・誤爆が観測不能になる）
- **セッション作成〜計測〜kill を 1 コマンドに詰め込まない**。破壊的操作は影響を確認できる単位で分けて打つ
- **逆に、隔離サーバは「human に回す」を減らす道具でもある**。tmux の挙動を human issue
  (動作確認待ち) に回そうとしたら、**その前に「隔離 `-L` サーバで測れないか」を一度問う** (人待ちは価値が腐る)
- 🚨 **`tmux kill-server` は socket ファイルを消さない**（実測 2026-09-10。SIGKILL でも同じ）。
  隔離サーバを止めるだけでは**正常終了のたびに socket が 1 個ずつ残る**ので、後片付けは
  「**kill する前に** `display -p '#{socket_path}'` で実パスを控え、kill 後にそのパスだけを
  `rm`」まで含める（正本は `tests/tmux/lib/kill_socket.sh` の `tt_tmux_kill_socket`）。
  `TMUX_TMPDIR` を使い捨て dir に倒して**dir ごと消す**形なら socket もその中なので追加の後始末は要らない
- **残骸を走査して消す掃除機構は作らない**。消すのは**自分が控えたパスだけ**にする
  （走査は母集合を取り違えた瞬間に本番の socket へ届く。`adversarial-review-own-safeguards.md` §0-A）
- **そもそも tmux を新規に立てる必要があるか先に問う**
- 恒久テストは tests/tmux/ の既存方式に倣う (冒頭の `unset TMUX TMUX_PANE` + `TMUX_TMPDIR`。`lib/isolate_env.sh` は HOME/XDG の隔離**だけ**で、socket は対象外)

## なぜ

根拠・起源・実例は `~/dotfiles/_claude/rules-rationale/tmux-probe-requires-socket-isolation.md` (起動時には読まれない。改訂するときに読む)。

## 強制手段（実装済みの部分）

- 🚨 **`bin/tmux` shim** が、本番サーバ (`default` と `~/.config/tmux-protected-sockets` の各行) への `kill-server` / `kill-session` を TTY の無いシェルから拒否する。PATH 先頭に居るので、スクリプト内部の bare 呼び出しも止められる唯一の層。対話 TTY と実体の絶対パス (`/opt/homebrew/bin/tmux`) は通す (本当に消すときの逃げ道)
- **PreToolUse hook** `_claude/hooks/deny-bare-tmux-kill.sh` が、Bash ツールで直接打った bare な kill・`pkill tmux`・`-L default` / `-S <...>/default` を deny する
- 隔離の実証・依頼外の破壊的操作・`2>/dev/null` は hook で強制できないので、この md が正本
- hook は引用符・コメントの外の散文も見るので、heredoc の本文 (`git commit -F -` の message) に kill の語と小文字の tmux が並ぶと偽陽性で止まる。message はファイル経由 (`git commit -F <path>`) で渡すか言い換える。パターン自体を書くテストでは、シェル変数でトークンを分割して組む

## 関連

- dotfiles の `scripts/CLAUDE.md`「サーバ状態に触るスクリプトの不変条件」— スクリプト側の同じ規律
