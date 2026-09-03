# `tmux_restore_runner.sh` だけ `exec </dev/null >/dev/null 2>&1` を持たない

起票日: 2026-08-26
種別: refactor
優先度: **P3** (現状で実害は確認できていない。非対称の理由が不明なのが問題)

## 確認できた事実 (2026-08-26)

`grep -L 'exec </dev/null'` で 3 本を比較した結果、**`scripts/tmux_restore_runner.sh` だけ**が
このリダイレクトを持たない:

| スクリプト | `exec </dev/null >/dev/null 2>&1` |
|---|---|
| `scripts/tmux_periodic_save.sh` | あり |
| `scripts/tmux_server_watchdog.sh` | あり |
| `scripts/tmux_restore_runner.sh` | **なし** |

3 本とも tmux の `run-shell` 文脈から起動される点は同じで、**無音契約 (出力を tmux へ返さない)
は 3 本とも同じはず**。`run-shell` はコマンドの出力があると view-mode を開いてしまうため、
これは体感に出る種類の契約 (`tmux_log_kill_command.sh` のテストにも「run-shell エラーの
view-mode 化防止」の assert がある)。

## どちらかが要る

1. **揃える** — restore_runner にも同じリダイレクトを入れる。ただし**挙動変更**なので、
   現在この経路の出力に依存している何か (デバッグ出力・エラーの伝播) が無いか確認してから
2. **理由をコメントで残す** — 意図的に持たせていない (例: 復元中の失敗を tmux 側に見せたい)
   なら、その理由をコード直近に書く (`pending-issue-rationale-in-code.md`)

**どちらが正しいかはこの調査では判定できなかった**ため、着手時に `run-shell` 経由で
restore_runner が出力を出すケースを実際に作って確認すること。

## 出典

2026-08-25 の issue 078 / 079 の作業中に並行セッションが気づいたもの。
コード上の非対称は本 issue の起票時に独立に再確認した。

## trigger

復元経路 (`tmux_restore_runner.sh` / `_tmux.conf` の restore hook) を次に触るとき。
issue 104 (復元経路の目視確認) と同時に見ると効率が良い。

---

## 対応 (2026-08-28): 「揃える」を選んだ

issue が「判定できなかった」とした二択に、**origin の証拠で決着がついた**:

`git show e089e7e` — **watchdog と restore_runner は同一コミットで作られ、`exec` 行は
watchdog にだけ入っている** (watchdog は `+1`、restore_runner は `+0`)。しかも
restore_runner の header には**初版から**「無音契約: run-shell -b 経由のため stdout/stderr は
汚さない」と書かれている。つまり**意図的な非対称ではなく、同一コミット内の書き漏れ**。
「理由をコメントで残す」側の証拠 (git log / issue / コメント) は 1 つも見つからなかった。

安全性も確認した: restore_runner は自分では stdout/stderr へ何も書かず、テストも見ておらず
(`stub_assert_helper.sh` が既定で捨てる)、stdin も読まない。

### 敵対的レビューが実測で崩した「私の理由づけ」

書いた理由のうち **2 つが誤り**だった (隔離ソケットで測定):

| 私が書いた理由 | 実測 |
|---|---|
| 「サーバが死ぬと SIGPIPE を受ける」 | **来ない**。SIGPIPE は書いたときにしか飛ばず、このスクリプトは stdout に 1 バイトも書かない |
| 「restore.sh が stdin を読むとブロックしうる」 | **起きない**。run-shell の子の stdin は即 EOF (`read -t 2` が rc=1。本物の timeout は 142)。resurrect の restore.sh も inherited stdin を読まない (12 箇所の `while read` は全て自前の入力を持つ) |

正しい理由は「**将来この経路が stdout へ 1 行でも出したら、復元中のユーザーのアクティブ pane が
view-mode になる**」への防御。パイプが終了まで生きること自体は実測で確認できている
(`sleep 3; echo` を `run-shell -b` すると 3.5 秒後に view-mode)。コメントを書き直した。

### さらに分かった、より重要な事実

| 子プロセス | pane が view-mode になるか |
|---|---|
| **stdout 1 行** | **なる** |
| stderr | **ならない** (tmux サーバの fd2 は `/dev/null`。本番 pid でも実測) |
| **出力なし + rc≠0** | **なる** |

→ **`2>&1` は view-mode 対策としては空振り**で、揃えるためだけに付いている。
→ **支配的な要因は rc≠0 の方で、`exec` では塞げない**。この経路は全て `exit 0` なので現状は
問題ないが、終了コードを変えるときはそこが本体、とコメントに書いた。

### テストで固定した

`tests/tmux/test_runshell_silence.sh` を新設。**この不変条件を守るテストは 1 つも無かった**
(`exec` を消しても全テストが green だった)。

🚨 字面の存在だけを見る形だと、末尾の死んだコードや heredoc 本文に同じ 32 バイトがあるだけで
緑になる (レビューが変異で実証)。**実処理 (`tt_trigger_log`) より前にあること**まで見る形にし、
死んだコードの変異が red になることを確認した。

### 範囲外にしたもの → [issue 129](129-refactor-runshell-silence-two-scripts-missing.md)

同じ基準に当てはまるのに未対応の 2 本がある (`tmux_schedule_keys.sh` の fire は**最大 30 日**
sleep する / `tmux_resurrect_debounced_save.sh` は既定 10 秒)。前者は起票時点で並行セッションが
編集中、後者は `tt_on_default_server` を関数内で呼ぶ構造で位置が機械的に決まらないため分けた。
**テストのコメントにも「この 3 本で全部ではない」と明記**した。
