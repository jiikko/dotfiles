# 128 research: schedkeys 監査 2026-08-28 の全数勘定と却下理由

起票日: 2026-08-28
種別: research
関連: [127](127-test-schedkeys-duplicate-and-coupled-tests.md) / 修正 commit 79e2453 / 70ed815

`/audit` を 6 タイプ (error-handling / resource-leaks / dead-code / test-cleanup / test-helpers /
performance) で直接実行した結果の勘定。**却下したものを残すのが主目的** (残さないと次の監査が
同じ指摘を再生成する)。

## 勘定

| 分類 | 件数 | 行き先 |
|---|---|---|
| 修正した | 20 | commit 79e2453 (P1 3 / P2 6 / P3 6 / テスト側 5 系統) |
| issue に残した | 5 | [127](127-test-schedkeys-duplicate-and-coupled-tests.md) |
| 却下 (下記) | 4 | このファイル |
| 「攻めたが壊せなかった」報告 | 多数 | 下記「壊せなかった範囲」 |

## 却下した指摘と理由

### 1. 「copy-mode では `send-keys -l` が rc=0 で成功扱いになる」→ **事実誤認**

指摘は「rc=0 なので成功として記録される」だったが、tmux 3.7b の実測では **rc=1**
(`not in a mode` を stderr に出す)。元のコメントと commit message (512f98e) の記述が正しい。
再測定手順: 隔離サーバで `copy-mode` に入れてから `send-keys -t <pane> -l -- 'echo X'; echo $?`。

### 2. 「`ansi.StringWidth` は肌色つき絵文字で tmux とずれる」→ **測る側の誤り**

監査中に掃引ハーネスが `200x40` で「211 桁 > 200」を報告したが、原因はハーネスが Python の
`east_asian_width` で数えていたこと (👍🏽 を 2 rune × 2 = 4 と数える)。**実 tmux は 2 セル**で描き、
`ansi.StringWidth` と一致する。13 種の書記素で ansi / ansiWc / tmux 実測を突き合わせた結果、
**全て ansi.StringWidth が一致** (ansiWc は 👍🏽=4, ❤️=1 でずれる)。アプリの数え方は正しい。

### 3. 「`jobs_tsv` / `prune_stale` の subprocess spawn が性能問題」→ **却下**

予約件数ぶんの spawn だが、popup を開いたときに 1 回だけ。10〜20 件で 300ms 未満、フレームごとの
コストではない。実測を伴わない複雑性の議論としても、方向はむしろ「問題ない」側。

### 4. 「`toast.done` / `holdCh` のガードは到達不能」→ **未確認のまま残す**

`toastDoneMsg` の後は `tea.Quit` を返すので届かないはず、という指摘。ただし bubbletea の非同期
スケジューリングで遅れて届く `toastTickMsg` の可能性を排除できず、再現も反証もできなかった。
消すと壊れたときに気づけないので現状維持。**発火条件を示せないので issue 化しない**。

## 攻めたが壊せなかった範囲 (次の監査の起点)

- `read_job` の REPLY_* 汚染 / pid 再利用による誤 kill / `fire_claim` の fail-closed /
  末尾 `;`・`#`・タブ・改行のエスケープ / UI 結果行のフィールド数検証 / `PID_GRACE_SECS` の猶予 /
  timespec の桁あふれ / `fitWidth` と editor の書記素処理 (無限ループ・幅超過) / `toast.overlay` の
  高さ保証 / Go の goroutine・timer・fd リーク / gum への option injection
- 実端末での掃引 129 通り (幅 10 種 × 操作 12 種 + 長い window 名) で崩れ 0。
  🚨 **この掃引の感度は限定的**: 高さ超過や幅超過を意図的に作った版でも端末上は崩れなかった
  (bubbletea v2 の描画側が切る)。幅・高さ・カーソルの不変条件を守っているのは Go 側のテストで、
  端末掃引が保証できるのは「UI が正しく描けている」ことまで。次に画面崩れを疑うときは、
  端末掃引の結果を根拠にしないこと

---

## 対応 (2026-08-28): 却下 4 のコード側の痕跡を足して done へ

この issue は**却下理由の保管**が目的で作業項目を持たない。ただし done へ移すと参照の重みが
下がるので、閉じる前に「却下した指摘がコード側からも棄却できるか」を 4 件とも確認した。

| 却下 | コード側の痕跡 |
|---|---|
| 1. copy-mode の `send-keys` は rc=0 で成功扱い (事実誤認) | ✓ `scripts/tmux_schedule_keys.sh` の送信箇所に「`not in a mode` で rc=1。tmux 3.7b で実測」と記載済み |
| 2. `ansi.StringWidth` が tmux とずれる (測る側の誤り) | ✓ `src/schedkeys/editor.go` の `fitWidth` に「StringWidth で測った幅で桁を組む」理由あり |
| 3. spawn が性能問題 (却下) | — 「問題なし」判定なので守るべき構造が無い |
| 4. `toast.done` / `holdCh` は到達不能 (未確認のまま現状維持) | **✗ 無かった** → 今回追加 |

4 だけコードに痕跡が無く、`holdCh` には「hold のタイマーを張ったか」としか書いていなかった。
このまま閉じると、次の監査か refactor が「到達不能な dead code だから消せ」を**再生成する**
(`pending-issue-rationale-in-code.md` が禁じている形)。`src/schedkeys/toast.go` の `advance` に
以下を残した:

- 何が守られるか: この 2 つのガードは **2 本目の `toastTickMsg` で `advance` が呼ばれたとき**に
  だけ効く。外すと遅れて届いた tick が 2 本目の hold タイマー (= 2 通目の `toastDoneMsg`) を張り、
  アニメーションの再開・二重終了が**無音で**起きる
- なぜ確定できないか: 通常は tick が 1 本しか飛ばない (`start` の呼び出し元は `model.go` の
  submit 1 箇所で、表示後はキーを受け付けない) が、bubbletea が quit の前に**予約済みの tick を
  配送しない**保証は確かめられず、再現も反証もできなかった
- どうなれば消してよいか: quit 後に tick が配送されないことをテストで固定できたとき

### 残課題

なし (この節をもって全数勘定が閉じた)。
