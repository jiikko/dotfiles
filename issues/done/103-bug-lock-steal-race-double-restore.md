# 103 bug: 取り残し lock の「奪う」経路が非アトミックで、復元が二重に走る

起票日: 2026-08-25 / 出典: issue 078 の統合に対する敵対的レビュー (実測つき)

## 症状

`~/.cache/tt-restore-run/lock` が **owner 死亡状態で残っている**とき、2 本の
`scripts/tmux_restore_runner.sh` が 1〜5ms ずれて起動すると **両方が lock 取得に成功し、
`restore.sh` が並行実行される**。観測ログには `restore-manual-begin` と `restore-end` が
2 行ずつ並ぶ。

この lock の存在理由そのもの (復元中フラグ `@tt-restore-in-progress` と pane 生成の競合。
2026-07-30 の 22/29 部分復元) が無効化される。

## 発火条件

1. 取り残しの lock がある (異常終了・SIGKILL・再起動跨ぎ)。
   restore の経路は `tt_lock_sweep_stale` を**呼ばない**設計なので、回収はこの奪取パスだけが担う
2. 2 本の runner が 1〜5ms 差で起動する (`C-t C-r` の連打 / auto-restore と手動の重なり)
3. 双方が `tt_lock_owner_alive` = false を観測 → 双方が `rm -rf` → 双方が `mkdir` に成功

## 実測 (2026-08-25)

| 条件 | 結果 |
|---|---|
| 関数レベル・B の遅れ 0.001s | 60/60 で二重取得 |
| 同 0.004s | 58/60 |
| 同 0.005s | 54/60 |
| 同 0s / 8ms 以上 | 0/60 (窓の外) |
| E2E (実 `tmux_restore_runner.sh`) | **40/40 で `restore.sh` が並行実行** |

**統合が持ち込んだ退行ではない**。`git show HEAD:scripts/tmux_restore_runner.sh` の旧実装でも
同条件で 20/20 再現する。issue 078 で 3 本のコピーが 1 関数
(`scripts/lib/tmux_resurrect_guards.sh` の `tt_lock_acquire`) に集約されたので、
**直す場所が 1 箇所になった**という位置づけ。

## なぜ即修正しなかったか

レビューが提案した「`rm -rf` + `mkdir` を `mv` + `mkdir` に替える」だけでは窓が閉じない:

- A: mkdir 失敗 → owner 不在 → `mv dir dir.stale.A` → `mkdir dir` 成功 → A が保持
- B: A が owner を書く前に owner 判定 → 不在と観測 → **`mv` が A の新しい lock を掴む** →
  `mkdir dir` 成功 → B も保持

「owner の生存確認 → 奪う」が非アトミックである限り、確認と取得の間に窓が残る。
`mkdir` は原子的だが、**奪取は「消す + 作る」の 2 段**なのでそこが穴。閉じるには
acquire/release を対で設計し直す必要があり、単独の設計 + 敵対レビューを別途通すべきと判断した。

## 併せて直すもの (同じ設計変更の中で)

**release が owner 無条件**。`tmux_periodic_save.sh` / `tmux_server_watchdog.sh` /
`tmux_restore_runner.sh` の trap は `rm -rf "$LOCK_DIR"` で、**先に終わった側がまだ走っている側の
lock を消す**。この repo は既に正解を持っている: `scripts/tmux_resurrect_save.sh` の
`tt_save_release_lock_if_owner` が「判定〜削除の間に別プロセスが取得した新 lock を消すと
並行 save を招く」という理由で条件付き解放になっている。`tt_lock_acquire` を共通化した以上、
対になる `tt_lock_release_if_owner` が無いのは非対称。

## 着手時の注意

- **変異検証の置き場は既にある**: `tests/zshrc/tmux-session/test_resurrect_lock_acquire.sh`
  (rc=0/1/2 と掃除条件を pin 済み)。レースの再現は 1〜5ms の窓を作る必要があるので、
  関数レベルのハーネス (2 プロセスを遅延つきで起こす) を足すこと
- コード側の痕跡: `tt_lock_acquire` の奪取箇所にこの issue 番号つきでコメントを残してある

## 反証できなかった観点 (レビューが「壊せなかった」と明記したもの)

- **統合の前後で挙動が変わる経路**: 旧版と新版を同一 fixture で 31 ケース実行し、
  rc / stdout+stderr / lock の残存と pid の中身 / 観測ログが**全ケース一致**
  (restore_runner 10 / periodic_save 14 / watchdog 7)。戻り値分岐化した restore_runner も
  ログ文言まで一致
- `tt_lock_sweep_stale` の glob が `$base` の外を消す経路 (空白入りディレクトリ含めクォート済み)
- `.lock` シンボリックリンク経由の破壊 (`rm -rf` はリンク自体しか消さない。`/etc` へのリンクで実測)
- `basename` が pid でない値を返す経路 (`0.lock` / `-1.lock` は `SERVER_PID` の数値制限で到達不能)
- shell 差 (guards.sh を source する 13 本はすべて bash。`#!/bin/sh` の
  `tmux_reap_orphan_servers.sh` は source していない)

## 未確認リスク

- `periodic_save` / `watchdog` の `<pid>.lock` での同レース。lock 名が生きているサーバ pid なので
  「同一サーバ pid の owner 無し lock が残っている」状況を自然発生させられず未確認。
  関数は共通なので理屈上は同じ窓があるはず
- `tt_same_proc` の fail-open (`ps -o lstart=` が生存 pid に空を返す環境) は macOS/Linux で再現不能

---

## 対応 (2026-08-28)

### 何が起きていたか (敵対的レビューで、最初の修正が原症状を閉じていないと判明)

最初の実装は「奪取そのものを `<dir>.steal` の `mkdir` で直列化する」だった。これは
**奪う側どうし**しか直列化しない。素の `mkdir` で入った新規取得者は `.steal` を持たないので、
奪う側から保護されない:

```
A: mkdir "$dir" 成功 → tt_lock_write_owner へ
   ⚠ write_owner は printf の前に $(tt_proc_starttime) で ps を fork する (実測 4.6ms)。
     この間 "$dir/pid" は存在しない
B: mkdir 失敗 → owner 不在 → 取り残しと誤認 → rm -rf "$dir" → mkdir → rc=0
A: 遅れて owner を記録 → **B の記録を上書き** → A の release が **走行中の B の lock を消す**
```

1 つの経路で 3 つ壊れる: ①二重取得 ②owner 記録の上書き ③走行中の他人の lock の削除
(③は `tt_lock_release_if_owner` が防ぐために新設した事象そのもので、防げていなかった)。

**なぜテストが緑だったか**: レースハーネスが毎回 `mkdir "$dir"` で取り残しを事前に作っており、
**両 worker が必ず奪取側へ入る**形状しか試していなかった。production の呼び出し元 3 本は
どれも **lock dir が存在しない状態から始まる**。実測 (2026-08-28):

| 形状 | 旧実装 | 最初の修正 | 今回の修正 |
|---|---|---|---|
| 取り残しを事前に作る (テストの形状) | 2 winners 30/30 | 0/30 | 0/30 |
| lock 不在から 2 プロセス (**production 形状**) | — | **2 winners 2/30** | 0/30 |
| 同上・`write_owner` の窓を 0.4s に拡大 | — | **15/15** | **0/15** |

### 直し方

**根は「`mkdir` と owner 記録が原子的でない」こと**なので、奪取の条件を
「owner 不在」から「owner 不在 **かつ** dir が TTL 超過」に変えた。作られたばかりの lock は
記録中かもしれないので奪わない。`.steal` による直列化は「TTL 超過の取り残しを複数が同時に
奪う」形に対して引き続き必要なので残した。

**回復の遅れは作らない**: 「owner が記録済みで死んでいる」(クラッシュした先任) と
「owner がまだ記録されていない」(記録中かもしれない) を **owner ファイルの有無**で区別し、
前者は従来どおり待たずに奪う。猶予が要るのは後者だけ。

同じレビューで出た 4 件も直した:

- **未来 mtime で永久ロックアウト** (`find -mmin -N` は未来 mtime にもマッチして「新しい」と
  判定する)。NTP のステップバック・スリープ復帰・VM の suspend/resume で発火し、呼び出し元 3 本は
  無音 exit 0 なので**保存も watchdog も二度と張られないのにログが 1 行も出ない**。
  `stat` ベース (BSD `-f %m` / GNU `-c %Y`) の年齢計算に置き換え、未来 mtime は「壊れている」
  として取り残し扱いにした
- **`find` 失敗時の fail-open** (出力が空 → 「古い」→ 奪取中の相手を蹴散らす)。上の置き換えで
  消え、mtime が読めないときは 0/1 に丸めず **rc=2 (判定不能)** を返すようにした
- **`.steal` 孤児が上限なく溜まる** (lock 名にサーバ pid が入るので同じパスは再訪されず、TTL 回収の
  機会が来ない)。`tt_lock_sweep_stale` が本体を消すときに道連れにし、本体が既に無い孤児も
  TTL 超過なら回収する
- **解放時に `ps` が失敗すると自分の lock を取り残す** (`tt_same_proc` は fail-open なのに
  `tt_lock_release_if_owner` だけ生文字列比較で fail-closed だった)。pid 一致だけで自分とみなす形に揃えた

### 自分で入れた退行 (既存テストが捕まえた)

新ガードを `[ ! -s "$dir/pid" ]` だけで書いたところ、**親が読み取り専用で `mkdir` が通らない**
ケース (dir が存在しない) までガードに入り、**「取得できなかった (rc=2)」が「先任がいる (rc=1)」に
化けた**。呼び出し元 3 本は rc=1 を無音で退くので、装置が張られないことがログに出なくなる。
`[ -d "$dir" ] &&` を足して既存の rc=2 経路へ落とした。

### 検証

- production 形状のレースを**窓を 0.4s に拡大して決定論的に**再現し、ガードの有無で
  **15/15 → 0/15** の A-B を取った (素の窓 4.6ms だと 2/30 でしか出ず確率的にしか落とせない)
- 変異 **9/9 red**。ガード除去 / 猶予を全 owner へ拡大 / `find -mmin` へ差し戻し /
  判定不能を rc=1 へ丸める / `.steal` を道連れにしない / 孤児回収ループの削除 /
  孤児回収の TTL 条件の除去 / 解放を生文字列比較へ差し戻し / `[ -d "$dir" ]` の削除
- テストに **production 形状 (lock 不在から 2 プロセス)** と、記録済みで死んだ owner の形状を追加。
  取り残しフィクスチャは**古くする**ように直した (作りたての owner 不在 dir は「記録中」と
  区別できないので奪えないのが正しい)

### 却下 / 未確認 (次の audit が再生成しないように残す)

- **奪取権 lock の TTL 奪取による多重取得**: A が奪取中に ~60 秒停止すると B が `.steal` を
  奪い、A の最終 `rmdir` が B の `.steal` を消す連鎖が理屈上ある。レビューは実験で示せず
  (スタブが `tt_lock_owner_alive` を汚染して両者 rc=2 になった)。**未確認リスクとして残す**
- **`tt_lock_acquire` の性能**: 呼び出しは各プロセスで 1 回だけ (3 本とも `while` ループの前)。
  非競合 6.73ms / 奪取経路 8.00ms で、支配項は元から `write_owner` の `ps` fork (4.6ms)。
  「periodic_save が 1.5 秒周期で呼ぶ」は成り立たず、年齢判定の追加コストは無視できる
- **3 / 5 / 8 プロセスの同時到達**: 取り残し形状では追加の穴なし。レビューが最初に出した
  「winners=3」はハーネス側の誤り (worker が lock を保持せず終了しており、後続が正当に再取得していた)
