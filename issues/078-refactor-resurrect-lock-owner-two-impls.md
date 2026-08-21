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
