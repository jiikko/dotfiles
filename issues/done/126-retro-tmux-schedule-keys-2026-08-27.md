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

6. **UI を gum から自作 TUI (bubbletea v2 / `src/schedkeys`) へ移した**。動機は IME の未確定文字が
   入力位置に出ないこと（gum = bubbletea v1 は本物のカーソルを隠す）で、pty で実測して確かめてから
   動いた。副産物として「いつ送る」と「文字列」が 1 画面になり、発火時刻をその場で見せられるように
   なった。**逃した順序**: 案 B（gum choose のプリセット）を実装した時点で「gum のままでよいか」を
   問い直していれば、gum 版のプリセット実装 → 絵文字剥がし → readline 化 → Go 化、と 4 回触らずに
   済んだ。UI の要求が 2 つ以上重なったら、道具を変える選択肢を先に検討する
   - 切り出し先の提案: `_claude/rules/` に「UI の作り直しは、要求が 2 つ重なった時点で道具の選択から
     問い直す」を新設するか、却下（一般化しすぎで発火条件が曖昧）。**却下寄り**
7. **層の切り分け**: Go には表示と入力だけを持たせ、job ファイル・tmux への副作用・破壊的な取消の確認は
   シェルに残した。おかげで既存のシェル側テスト（fire / prune / 取消の fail-safe）がそのまま生き、
   移行で失った不変条件が無い。切り出し不要（この issue に記録として残す）

8. **テストがそもそも保存されていなかった**（2026-08-28）。`cat >> src/schedkeys/model_test.go` を
   cwd がずれた状態で実行し、リダイレクトが失敗していたのに気づかず「テスト追加済み」として
   変異検証まで進めた。変異が 3 本 GREEN になって初めて発覚（`[no tests to run]`）。
   - 切り出し先の提案: `verify-execution-not-just-exit-code.md` に「ファイルを書いたら、書けた証拠
     （実在・行数・テスト名が実行一覧に出ること）を見る」を 1 行。**または却下**（既存の
     「実行された証拠で判定する」で覆えているとも言える。判断はユーザー）
9. **敵対的レビューの ROI が高かった**（2026-08-28、ラウンド 2 = 観点 2 つ並行）。自分の変異検証を
   13 本通した後でも、P1 が 3 件出た: UI が無限ループで固まる（幅 0 の書記素）/ サーバ異常終了後に
   別サーバの pane へ送信 / 低い画面でカーソルが枠外。いずれも「自分が想定した不変条件の外側」で、
   変異検証では原理的に出ない。
   - 切り出し先の提案: `mutation-verify-new-tests.md` と `adversarial-review-own-safeguards.md` の
     相互リンクに「変異は自分の想定内しか試さない」を 1 行（項目 5 と同じ提案。統合してよい）
10. **レビューの指摘は実測で裏が取れないと形が変わる**。「copy-mode では rc=0 で成功扱い」→ 実際は
   rc=1 で失敗し「pane が消えた」と誤報。「サーバ pid を見る」→ 眠る**前**に見ていて実機では効かず、
   E2E で初めて発覚。**指摘の対策を入れた後、その経路を実機で 1 回通す**のが要る。
   - 切り出し先の提案: 既存ルールで覆えている（`verify-execution-not-just-exit-code.md`）。却下候補

## 切り出しの結果 (2026-08-29 実施)

- **項目 1 → 却下**。tmux の bind 撤去は `_tmux.conf` の「撤去済み bind の掃除」節のコメントと
  `test_schedule_keys.sh` の pin (`unbind -T prefix Enter`) で足りる。ルール化しない
  (tmux 固有で汎化しない)
- **項目 2 → 却下 (見送り)**。`scripts/CLAUDE.md` の無音契約への 1 行追記は今回入れない。
  🚨 **知見は失っていない**: リダイレクト自体の失敗 (書き込み先ディレクトリ不在) は
  `cmd >> f 2>/dev/null` では隠せず、`{ cmd >> f; } 2>/dev/null` で囲む必要がある
  (リダイレクトはコマンド実行前に評価されるため)。同じ形で 2 回目に踏んだら追記する
- **項目 3 / 7 → 切り出し不要** (本文に記録として残す)
- **項目 4 → 実施**。[`tmux-probe-requires-socket-isolation.md`](../../_claude/rules/tmux-probe-requires-socket-isolation.md)
  の「ルール」に「**隔離サーバは human に回すのを減らす道具でもある**」を追記した
  (human issue に回す前に「隔離 `-L` サーバで測れないか」を一度問う)
- **項目 5 / 9 → 実施 (統合)**。`mutation-verify-new-tests.md` の「関連」と
  `adversarial-review-own-safeguards.md` の「関連」に相互リンクを張り、
  「**変異は自分が想定した不変条件しか試さない**。変異を全部 red にした後でも敵対レビューは
  想定の外側の P1 を出す (実測 2 回)。片方で閉じず両方通す」を明記した
- **項目 6 → 却下**。「UI の要求が 2 つ重なったら道具の選択から問い直す」は一般化しすぎで
  発火条件が書けない (本文の「却下寄り」判断を採用)
- **項目 8 → 却下**。「ファイルを書いたら書けた証拠を見る」は
  [`verify-execution-not-just-exit-code.md`](../../_claude/rules/verify-execution-not-just-exit-code.md)
  の「実行された証拠で判定する」「新規テストはテスト名が実行一覧に出ることを確認する」で覆えている
- **項目 10 → 却下**。「指摘の対策を入れた後、その経路を実機で 1 回通す」も同ルールで覆えている

## 残課題

なし (全項目が却下・実施・記録で閉じた)
