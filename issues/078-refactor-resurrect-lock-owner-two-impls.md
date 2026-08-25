# 078 refactor: resurrect 系 lock の owner 同一性判定が 2 実装 / 常駐 lock の取得手順が逐語重複

起票日: 2026-08-21
種別: refactor
優先度: **P2** (068 の直接原因と同根。書式が片方だけ動くと無音で「先任が生きている」誤認に戻る)

出典: 監査 [070](done/070-research-quality-audit-2026-08-20.md) の `070-two-owner-impls` /
`070-guards-empty-owner-steal`、[071](done/071-research-design-audit-2026-08-20.md) の
`071-lock-acquire-verbatim-dup`。監査時は未裏取りだったので 2026-08-21 に実コードで確認した。
**出典 issue には「反証で崩れた (却下)」の一覧がある。同型の指摘を再提案する前にそちらを読むこと**
(裏取り・反証レポートは `./tmp/` に出したが gitignore で失われており、出典 issue が唯一の記録)。

## 確認できた事実 (2026-08-21)

**owner 同一性判定が 2 実装ある**:

- `scripts/lib/tmux_resurrect_guards.sh` の `tt_lock_write_owner` / `tt_lock_owner_alive`
  (+ `tt_same_proc`) — periodic_save / server_watchdog / restore_runner / snapshot_health が使う
- `scripts/tmux_resurrect_save.sh:97-148` の `tt_save_proc_starttime` / `tt_save_owner_is_stale`
  / `tt_save_lock_older_than` — save 側だけの独立実装。指紋の作り方が違う
  (`ps -o lstart= | tr -s '[:space:]' '_'`)

[068](done/068-bug-snapshot-health-lock-owner-format-drift.md) は「owner 書式の出典が複数ある」
ことが原因だった。バグ自体は直したが**出典の二重化は残っている**ため、片方の書式を変えると
同型の drift が再発する。

**取得手順が逐語重複**: `scripts/tmux_periodic_save.sh:62-77` と
`scripts/tmux_server_watchdog.sh:63-78` が「stale lock 掃除ループ → `mkdir` → 先任生存なら
exit 0 → `rm -rf` → 再 `mkdir` → `tt_lock_write_owner`」をディレクトリ名とメッセージ以外
逐語同一で持つ。`tmux_restore_runner.sh:47-55` は掃除ループのない縮小版。

## 下がる複雑性

- owner 書式の touch 箇所が 2→1 (drift の構造的な余地が消える)
- 常駐 lock の取得規律 (pid 再利用の窓を開けない順序) の touch 箇所が 3→1。この順序は
  watchdog のコメントが「装置不在側に倒れる」と実証済みの load-bearing な規律で、
  コピーが増えるほど片方だけ直す事故に効く

## 対応方針 (着手時に再確認)

1. guards.sh に `tt_lock_acquire <dir> <pid>` 相当を置き、periodic_save / watchdog / restore_runner
   をそこへ寄せる (掃除ループの有無の差は引数か別関数で表現する。**restore_runner に掃除を
   後付けで足すのは挙動変更**なので分けて判断する)
2. save 側の owner 判定を guards.sh の実装へ寄せる。ただし save 側には
   「生存 owner は絶対に奪わない + mtime hard TTL の backstop」という独自の妥協があり、
   guards.sh 側にその概念があるか確認してから統合する (素朴な置換は保存停止 or 誤奪の
   どちらかを作る)
3. `070-guards-empty-owner-steal` (owner 行が空のときの奪取) は統合の中で扱う。単独では
   発火条件が示されていないため、統合の副作用として塞げるかだけ見る

## 変異検証

統合後、以下で red になることを確認してから閉じる: (a) `tt_lock_owner_alive` を常に真に
する → 二重起動ガードのテストが赤 (b) 常に偽にする → 先任を奪うテストが赤
(c) owner 書式のフィールド区切りを変える → drift 検出テストが赤 (068 の回帰テストが
これを担っているか確認する)

## trigger

lock 周辺 (guards.sh / periodic_save / watchdog / resurrect_save の lock 節) を次に触るとき。
先に触るのは 2 (owner の二重実装) で、1 は挙動変更の判断が要るので後。

## 対応 (2026-08-25)

**指紋 (owner 同一性判定) の出典を 1 つにした。取得手順の逐語重複 (項目 1) は見送り。**

### 実施: 指紋の単一出典化 (項目 2)

`tt_save_proc_starttime` (save 側の独立実装) を削除し、`tt_proc_starttime` (guards.sh) に寄せた。

⚠️ **書式が実際に食い違っていた**ことを実測で確認した:

| | 出力 |
|---|---|
| guards `tt_proc_starttime` | `火  8/25 17:09:37 2026     ` (生の ps 出力・**空白入り**) |
| save `tt_save_proc_starttime` | `火_8/25_17:09:37_2026_` (空白を `_` に正規化) |

save 側は owner 行を `read -r owner_pid owner_start` で**空白分割**して読むため、guards の書式が
混ざると先頭語 (`火`) だけが記録され比較が壊れる。統合先は **正規化する側 (save の書式)** を採り、
guards の `tt_proc_starttime` が正規化するようにした。

⚠️ 2 系統は lock dir が別 (`$TT_SAVE_STATE_DIR/lock` と `$TT_PERIODIC_STATE_DIR/*.lock`) なので、
この drift は**潜在**で実害は出ていなかった。068 と同じ「出典が 2 つある」状態を閉じたのが本質。

### 移行ガード (誤奪の防止)

書式を変えると、**既存の lock ファイル (旧書式) を「別プロセス」と誤判定して生存 owner を奪う**
経路ができる。実測で「旧書式は先頭語 `火` だけが残り、新トークンと必ず不一致になる」ことを
確認したうえで、両側にガードを入れた:

- guards `tt_same_proc`: 記録側も同じ正規化を通してから比較する (旧書式のまま一致する)
- save `tt_save_owner_is_stale`: 正規化済みトークンは必ず `_` を含むので、含まない記録は
  「同定不能」に落として **mtime hard TTL backstop** に委ねる (奪わない側へ倒す)

### ⚠️ 一本化だけでは守られなかった (変異検証)

統合直後に変異を当てたところ、**5 本中 4 本が green のまま通った**:
起動時刻の比較を `return 0` に潰す / 移行ガードを外す / 死亡 pid チェックを外す /
lock の owner 書式を変える。**guards.sh 側の指紋比較を触るテストが 1 本も無かった**ため。

`tests/zshrc/tmux-session/test_resurrect_owner_fingerprint.sh` (10 観点) を新設して固定した。
再実行で **5 本すべて red** になることを確認済み。

### 見送り: 取得手順の逐語重複 (項目 1)

`tmux_periodic_save.sh` / `tmux_server_watchdog.sh` の「掃除ループ → mkdir → 先任生存なら exit 0
→ rm -rf → 再 mkdir → owner 書き込み」の逐語重複と、`tmux_restore_runner.sh` の縮小版
(掃除ループなし) の統合。**issue 本文の警告どおり、restore_runner に掃除ループを後付けするのは
挙動変更**で、保存停止 (掃除しすぎ) か誤奪 (掃除の条件が緩む) のどちらかを作りうる。指紋の
統合とは独立に判断できるので分けた。

⚠️ また、save 側の `tt_save_owner_is_stale` を guards の `tt_lock_owner_alive` で**置き換えては
いけない**ことも確認した。guards 側は「起動時刻が取れない環境」で fail-open (= 生存扱い) のまま
永久に待つが、save 側にはそこに **mtime hard TTL backstop** がある。置き換えると PID 再利用時に
全保存経路が exit 1 を繰り返す (= 保存停止) 経路が復活する。**判定の骨格は共有、政策層は save
固有**、という分担が正しい。

## 追記 (2026-08-25): 移行ガードが platform 依存で破れていた

上の統合を push したところ **CI (Linux) だけ赤**になった (手元 macOS は緑)。移行ガードの
ケースだけが落ちる形。

根因は**正規化が末尾空白に依存していた**こと。`ps -o lstart=` は **macOS が末尾を空白で
パディングし、Linux はしない**:

| | 生 ps 出力 | `tr -s '[:space:]' '_'` 後 |
|---|---|---|
| macOS | `火  8/25 18:09 2026␣␣␣␣␣` | `火_8/25_18:09_2026_` (末尾 `_` あり) |
| Linux | `Tue Aug 25 18:09 2026` | `Tue_Aug_25_18:09_2026` (末尾 `_` なし) |

`tt_proc_starttime` 側は `$(...)` が末尾改行を剥がす前に `tr` が `_` へ変えるので末尾 `_` が
残る。記録側 (旧書式) は `$(...)` が末尾改行だけ剥がすので、**macOS ではパディングが `_` に
なって一致し、Linux では一致しない**。

→ これは**テストの問題ではなく本番のバグ**だった。Linux では移行ガードが機能せず、
**生存 owner の lock を奪う**。`tt_norm_fp` (空白を潰した上で前後の `_` を落とす) を導入して
platform 非依存にした。

**テストに「両 platform の形状」の観点を足した** (末尾パディングあり / なし / 先頭空白)。
手元の 1 platform で緑を見ても platform 依存は捕まらないため、両方の形状をテスト内で作る。
修正前の形へ戻す変異で red になることを確認済み。

## 残課題

- [ ] lock 取得手順の逐語重複の統合 (restore_runner の掃除ループ有無をどう扱うか決めてから)
