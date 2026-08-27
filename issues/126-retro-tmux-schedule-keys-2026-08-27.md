# 126 retro: prefix+m 予約入力ウィザード (tmux_schedule_keys) セッションの振り返り

起票日: 2026-08-27
種別: retro
関連: commit 683831c / [125](125-human-verify-tmux-schedule-keys.md)

## 気づき

1. **conf から bind を消しても、稼働中サーバの bind は消えない**（557ebca で launcher を撤去したのに
   reload 後も prefix+Enter で出続けた）。`source-file` は既存 bind を触らないので、撤去した bind は
   conf に `unbind` を残す運用にした（`_tmux.conf` の「撤去済み bind の掃除」節）。
   - 切り出し先の提案: `_claude/rules/` に「tmux の bind を撤去したら unbind を conf に残す」を足すか、
     `_tmux.conf` のコメントで足りるか。**提案: conf のコメント + テスト
     (`test_schedule_keys.sh` が `unbind -T prefix Enter` を pin) で十分。ルール化は却下候補**
     （tmux 固有で汎化しない）
2. **無音契約の破れはテストが最初に見つけた**: `log()` の `>> "$LOG" 2>/dev/null` は
   リダイレクト先ディレクトリ不在の失敗を隠せない（リダイレクトはコマンド実行前に評価されるので
   `2>/dev/null` の順序が効かない）。「pane 消滅 → stdout/stderr 空」の assert で red になり、
   `{ ...; } 2>/dev/null` に直した。
   - 切り出し先の提案: `scripts/CLAUDE.md` の無音契約の節に 1 行（「リダイレクト自体の失敗は
     `{ cmd >> f; } 2>/dev/null` で囲まないと stderr に漏れる」）。小さいので同節への追記が妥当
3. **`make test-pipefail-grep-q` ゲートが新規テストの `| grep -q` を止めた**（issue 096 の再発防止が
   機能した実例）。自分で気づかず gate に拾われたので、gate の価値の実測として記録。切り出し不要
4. **「reload で sleeper が死ぬか」を human issue に回しかけたが、隔離 -L サーバで 1 分で実測できた**
   （生存する）。人に回す前に「隔離サーバで測れるか」を一度問うべきだった。
   - 切り出し先の提案: `issues/README.md` の human 節に「tmux の挙動は -L 隔離サーバで測れるものを
     先に潰してから human に回す」を 1 行足す、または却下（既存の
     `tmux-probe-requires-socket-isolation.md` が隔離の作法を持っているので、そこへ「human 回避の
     手段としても使う」を 1 行足す方が置き場として自然）

5. **敵対的レビュー (sonnet 1 体、read-only) の結果**: P1 3 件が本物だった。
   - 一覧の取消を表示文字列で逆引き → 表示が一致する 2 件で先頭に化ける（行頭の連番で選ぶ形に修正）
   - `.pid` の数字だけで `kill` → pid 再利用で無関係なプロセスを kill（`ps -o command=` で
     「自分の `fire <id>`」を確認してから kill / 生存判定。無関係なら stale として掃く）
   - pane 消滅の事前チェックと send-keys の間の TOCTOU → send-keys の stderr が無音契約を破り、
     成功ログも無条件に書いていた（事前チェックを捨て、send-keys の rc で分岐。stderr は捨てる）
   - P2: bare tmux の socket（job に `socket_path` を残し fire が `$TMUX` に載せる）/ 送信途中の
     kill で文字列だけ残る（送信前に `trap '' TERM`）/ 桁数上限なしで 64bit あふれ（5 桁上限）
   - 却下: `$SELF` に `'` が入るとの指摘 — dotfiles のパスは固定で `'` を含まない。対応しない
   - 気づき: **自分のミューテーション 6 本が全部 red でも、レビューは P1 を 3 つ出した**。変異は
     「自分が守ろうとした不変条件」しか試さない。レビューで初めて出た不変条件（pid 再利用・
     表示一致・TOCTOU）は全て「複数の主体 / 時間差」が絡むもので、単体の stub テストからは
     出てこない。切り出し先の提案: `mutation-verify-new-tests.md` に「変異は自分の想定内しか
     試さない。並行・時間差・外部プロセスが絡む安全機構は敵対レビューを別に通す」を 1 行
     （既に `adversarial-review-own-safeguards.md` が言っているので、そちらへの相互リンク追記で足りるかも）

## 残課題

- [ ] 上記 1〜5 の切り出し先の判断（ユーザー）
