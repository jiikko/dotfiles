# `Renew` / `Release` も「照合してから名前に対して破壊的操作」で他人の lock を壊す

起票日: 2026-09-15
カテゴリ: bug / priority: **high**
対象: `src/lockman/lock.go` の `Renew` / `Release`
出典: [issue 366](done/366-bug-lockman-stale-takeover-sometimes-has-two-winners.md) の横展開
反証レビュー: 未実施

## 問題

366 で直した `tryTakeover` と**同じ形**が 2 箇所残っている。どちらも
「lock を読んで token と期限を照合する」→「**その後で lock という名前に対して**
破壊的操作を打つ」構造で、照合から操作までのあいだに引き継ぎが挟まると、
**別人の lock を壊す**。

- `Renew`: `O_WRONLY|O_TRUNC` で開いて書き直す → 引き継いだ**別人の lock を自分のメタで上書き**する
- `Release`: `os.Remove(l.lockPath())` → 引き継いだ**別人の lock を消す**

`Renew` 側は既にコード内コメントで既知の窓として記録されており、そこには
「**再開の trigger: 実 lock を使った並行実験でこの上書きを再現できたとき**」と書いてある。
下記の実測でその trigger は**発火済み**。`Release` 側は未記録だった。

## 実測 (2026-09-15 / darwin arm64 / go1.25.4)

366 と同じ手口 (期限検査の直後に seam を置いて順序を決定論にする) で**両方とも再現した**。
seam は一時的に入れて実験後に外してある (commit していない)。

手順はどちらも同じ:

1. A が短い TTL (50ms) で acquire する
2. A が `Renew` / `Release` を呼び、期限検査を**通った直後**で止める
3. TTL が切れるまで待ち、B が引き継いで自分の lock を置く
4. A を再開させる

結果:

| 経路 | A の戻り値 | 観測 |
|---|---|---|
| `Renew` | `nil` (成功) | lock の中身が **B の token から A の token + label=A へ**書き換わった |
| `Release` | `nil` (成功) | **lock が消えた** (B は自分が保持していると思っている) |

どちらも A は「成功した」と報告する。`Release` の側は、消えた直後に第三者が acquire に
成功するので**二重実行に直結する**。

## 着手前に分かっていること (再導出を省くため)

- **`Release` と `tryTakeover` の競合は閉じている**。`serverNow` を取る順序が逆で、
  `tryTakeover` は `readLock` の**前**、`Release` は**後**に取る。takeover 側の判定が
  保守側 (古い now) に倒れているので、takeover が期限切れと判定した後の `Release` は
  `serverNow` が単調なかぎり必ず期限切れ側に落ちて `errNotOwner` で帰る。
  **この issue で直すのは「引き継がれた後に Release / Renew を呼ぶ保持者」の側**であって、
  takeover との競合ではない (2026-09-15 に issue 366 の敵対的レビューが確認、コードで裏取り済み)
- **`Break` は無条件**。期限検査も token 照合も目印の取得もしない `os.Rename` なので、
  同族の窓を「時計の際どさ無しで」開ける。366 では `Break` を意図的な force break として
  受容し、コメントに明記した。この issue で扱うかは着手時に判断する

## 2026-09-16 追記: 発火に「第三者のタイミング」は要らない — 自分の deferred Release が窓を開ける

366 の敵対的レビュー (観点③) より。上の実測は seam で順序を作ったが、**実運用では
`lockman with` が自分で窓を開ける**列がある:

1. tick → `renewAsync` の goroutine G が `Renew` に入る。`readLock` と期限検査は通る
   (まだ自分のもの)。G は `O_WRONLY|O_TRUNC` の open でブロック
2. `--io-timeout` 発火 → `reportRenewErr` → `escalateGroupKill` → 子が死ぬ → `runWith` が return
3. **`runWith` の defer `ReleaseTimed` が lock を削除する**
4. `dispatch` の defer `CleanupTimed` が最大もう 1×io-timeout 走る。**その間 G は生きている**
   (`withTimeout` は固まった goroutine を回収しない)
5. その窓で別プロセス P2 が acquire → 新しい lock (token T2) を置く
6. G の open が解けて **P2 の lock を truncate し、T1 の meta を書いて Sync**

被害は 380 本文の「上書き」だけではない:

- P2 の次の `Renew` は `m.Token != token` → `errNotOwner` → **既定の `--on-lost=kill` で
  健全な子を SIGTERM/SIGKILL する** (無関係なジョブが殺される)
- P2 の `Release(T2)` も `errNotOwner` → **誰も解放できない lock が TTL ぶん残る**

🚨 **未確認**: 手順 6 で、stall 中の `open(2)` が「削除後に作り直された新しい inode」を
開くかはパス解決の再試行に依存する (名前ベースの操作なので成立するはずだが実機未確認)。

## 2026-09-16 追記: 381 の修正で「遅れて書きに来る Renew」の本数が 1 → 最大 8 になった

[381](381-bug-lockman-with-renew-latch-stops-renewal-forever.md) を直すまで、`with` の
更新は**同時 1 本**しか存在しなかった (期限切れのあとも `renewCh` を握っていたため、詰まった
1 本が返るまで次を積まなかった)。381 はその恒久ラッチを外し、期限が来たら見捨てて次の tick で
新しい更新を積む形にしたので、**見捨てられた `Renew` が最大 `maxInFlightRenews` (既定 8) 本、
同時にブロック中**になりうる。

見捨てられた 1 本は解放されたときに `lockPath()` を**名前で開き直して** `O_TRUNC` + write を
打つので、本 issue の窓は「1 本ぶん」から「最大 8 本ぶん」に広がった。さらに、**同一プロセス内の
2 本の `Renew` が同じ lock へ並行して `O_TRUNC` + write する形**は 381 以前には構造的に
存在しなかった (読み手は `readLock` が壊れた JSON を `errBusy` へ倒すので「空いている」には
ならないが、他人の lock を上書きする側は変わらない)。

優先度の判断材料が変わったので、本 issue を後回しにするなら `maxInFlightRenews` を
下げることも併せて検討する (正本は `src/lockman/with.go` の同名の定数コメント)。

## 直し方の候補

366 で採った形 (観測した世代から決まる名前を O_EXCL で取って調停 → 破壊的操作の直前に
再照合) がそのまま当たるかは未検討。ただし `Renew` は**保持中に何度も呼ばれる**ので、
呼ぶたびに調停の目印を作る形はコストが違う (366 の `tryTakeover` は引き継ぎのときだけ)。

- `Renew`: 366 の `tryTakeover` と同じ「rename で勝者を 1 人に絞る」形を持ち込む案が
  元のコメントに書かれている (renew のたびに rename が増える)
- `Release`: 「自分の lock だけを消す」は、消す直前に再照合しても窓は残る。
  `renamex_np` のような「対象を指定した」原始操作は POSIX に無い

🚨 **366 の修正で `tryTakeover` 側は閉じたので、この 2 経路が残る最後の同型**という
主張は**未検証**。着手時に `lock.go` を全数で洗い直すこと (grep 1 回で確定させない)。

## 影響

`av1ify` は finalize の直前に `renew` の rc で保持を判定する (`__av1ify_lock_still_held`)。
`Renew` が「他人の lock を上書きして成功を返す」ため、この判定は**保持していないのに
保持していると答える**ことがある。

## 残タスク

- [ ] `Renew` の窓を閉じる (または閉じないと決めて、コメントの trigger を更新する)
- [ ] `Release` の窓を閉じる (または閉じないと決めて、理由をコードへ残す)
- [ ] `lock.go` に同型が他に無いかを全数で洗う
