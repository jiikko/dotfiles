# macOS 連携 (Karabiner-Elements / kernel-alloc-watch / Finder Quick Actions)

## Karabiner-Elements

設定の正本は [mac/karabiner.json](../../mac/karabiner.json)（ANSI 基準。`bin/restore_karabiner_config.sh`
が適用時にマシンの実キーボード (JIS/ISO) を判定してパッチする。`bin/backup_karabiner_config.sh` は
live 設定の丸ごとコピーなので、JIS マシンで実行すると ANSI 基準が壊れる点に注意）。

- complex modifications は `make test-karabiner` (karabiner_cli) で意味レベル lint される
- simple_modifications の **`japanese_eisuu` → `a` は意図的なマッピング**（愛用中。削除しないこと。
  英数切り替えはコマンドキー単押し・Ctrl+T 等の complex rule 側が担っている）

## カーネルメモリの漏れの記録 (kernel-alloc-watch)

カーネルの zone `data.kalloc.1024` の在庫を記録する (issue 500。漏れるとパニックする。プロセス一覧には出ない)。

```bash
kernel-alloc-watch            # 人が叩く形: 1 行記録して、在庫・増え方・判定 (正常 / 要観察 / 漏れの疑い / 漏れている / 危険) を出す
kernel-alloc-watch snapshot   # Claude やスクリプトが叩く形: 1 行記録して、判定を JSON 1 行で出す
kernel-alloc-watch list       # 記録を古い順に出す (時刻 / inuse / MiB / 前の行との差 / claude の数 / tmux のクライアント数)
kernel-alloc-watch destroy-all-logs --yes   # 記録を全部消す (--yes 無しなら消すものを出すだけ)
kernel-alloc-watch --help     # 判定の閾値・列の意味・JSON の項目・終了コード
```

- 記録先は `~/.cache/kernel-alloc-watch/log.tsv` (`KERNEL_ALLOC_WATCH_DIR` で変更)。記録のたびに 7 日より古い行を落とす
- 増え方は直近 6 時間の最初の記録から今までの平均。直近の記録の幅が 10 分に満たなければ、6 時間より前で最も新しい記録からの平均に倒す。起動より前の記録は使わない (再起動で在庫が戻るため)。詳しくは `kernel-alloc-watch --help`
- claude の数と tmux のクライアント数は、増えた時間帯に何が動いていたかを突き合わせるための手がかり (数えられないときは `-`)

## Finder Quick Actions

Finderの右クリックメニューから動画処理コマンドを実行できます。

```bash
# セットアップ
~/dotfiles/mac/finder-actions/setup-concat-finder-action.sh
```

詳細は [mac/finder-actions/README.md](../../mac/finder-actions/README.md) を参照。
