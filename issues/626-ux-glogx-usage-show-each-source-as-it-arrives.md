# 626 (ux): glogx の利用枠 (U / R) を、Claude と codex の両方がそろうのを待たず、取れた方から出す

起票日: 2026-10-02

## 概要

glogx の利用枠の表示 (右上の U の箱 / 全画面の R のダッシュボード) は、`usage.FetchAll` が Claude と codex を並列に取ったあと
両方の結果を 1 つの Snapshot に併合してから 1 通の `usageMsg` で届く。速い方 (codex app-server は 0.6〜1.2 s、claude は約 2 s。
`usage_overlay.go` の fetchCmd のコメントの実測) が先に返っても、遅い方 (最大 `fetchTimeout` = 10 s) を待ってから描かれる。
ユーザーの要望 (2026-10-02): 取得した方から表示していく。

## 対応方針

- usage パッケージ: 出所ごとの取得 (Claude / codex) と、出所ごとの結果を Snapshot へ入れる併合を 1 か所に置き、`FetchAll` もそれで組む
  (bin/ratelimit と hook の振る舞いは変えない)
- glogx: fetchCmd は 2 本の取得を同時に投げ (Cmd が `tea.BatchMsg` を返す)、`handle` は届いた出所の枠だけを入れ替える
  - last-good は出所ごと (今の `MergeLastGood` と同じ不変条件)。全滅 (両方失敗) の扱い・`staleErr` / `ClaudeErr` の注記は今と同じ意味を保つ
  - single-flight は「その周の 2 本が両方返るまで」。キャッシュの保存は周の終わりに、その周で取れた分だけで行う (Claude 必須の契約は今のまま)
  - 片方を待っている間は、待っている出所を「取得中」として出す

## 進捗

- 2026-10-02: 起票
- 2026-10-02 「feat(glogx): 利用枠を Claude と codex の取れた方から出す (626)」
  - usage: 出所ごとの取得 `FetchClaudePart` / `FetchCodexPart` と、出所ごとの結果を入れる `Snapshot.With` (書き換えずに新しい値を返す。
    出所ごとの last-good・ClaudeErr・Claude → codex の並び) を置いた。`FetchAll` / `MergeLastGood` は本番の呼び出し元が 0 になった
    (bin/ratelimit は `Fetch` / `FetchCodex` を直接呼ぶ) ので消し、その stub テストは Part の形へ移した
  - glogx: fetchCmd は 2 本を `tea.BatchMsg` で投げ、handle は届いた出所を `usageRound` (周の始まりの表示 + 届いた分) から表示へ入れる。
    初回の取得だけ、まだの出所を U の箱の行と R の見出しの下に「⠋ Claude Code 取得中...」と出す (定期リフレッシュでは出さない)。
    キャッシュの保存は周の終わりに、その周で取れた分だけ (Claude 枠があるとき) を書く。全滅の周は周の始まりの表示へ戻して staleErr
  - 変異 (すべて red、mutate-verify-list): ClaudeErr を立てない / 消さない / 並びを codex 先に / 周の途中で表示へ入れない /
    何も取れていないのに表示を作る / 全滅で戻さない / 理由を届いた順に / 保存に last-good を混ぜる (2 通り) / リフレッシュで「取得中」/
    箱の「取得中」の行を出さない / スピナーが待ちを見ない / 盤の描画キャッシュの鍵から待ちを外す / tui が待ちを渡さない /
    R のスピナーが待ちを見ない / 回復の err を下ろさない / staleErr を早く消す・消さない / codex の失敗を ClaudeErr に / codex のバージョンを落とす
  - 敵対的レビュー (opus、2 周): 1 周目 P2 3 件 = テストの穴 (部分の回復・codex の失敗で ClaudeErr・保存の last-good) → テストを足して変異で red。
    P3 = 全滅の後の周で先に届いた方が staleErr を消し、Claude の前回の値が注記なしで最大 10 s 出る (再現あり) → staleErr は周の終わりに下ろす形に直した。
    P3 = usageMsg に周の番号が無い → 閉じた周の後に part が届く経路は今は無い (endRound は 2 本そろったときとキャッシュの hit だけ) ので直さず、
    handle の doc に「経路を足すなら先に番号を持たせる」と書いた。コメントの古い記述を直した。
    2 周目: 壊せなかった。P3 = 周の途中で codex が新しくても「前回の値を表示中」が残る (過剰な警告の向きなので受ける) /
    codex のバージョンが未検査 (変更前から) → 検査を足した
  - `make -C src/glogx test` / `lint`、`make -C src/ratelimit test` / `lint` rc=0。`-race -count=3` (Usage / FetchCmd / RLDash / Ratelimit) rc=0
  - root の `make test` は rc=2: 落ちたのは `tests/bin/test_kernel_alloc_watch.sh` の 1 件だけ (「ロックを握る側が 20 秒たってもロックを取れない」)。
    並列の負荷の下でだけ出て、単体では rc=0。今回の変更 (src/glogx・src/ratelimit) は触っていない
  - 残り: 実機の glogx での見え方 (起動直後に codex が先に出て、Claude が後から入る) は未確認
