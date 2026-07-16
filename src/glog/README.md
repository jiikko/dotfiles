# glog

GitHub Actions / GitHub Checks の結果をコミットごとに添える `git log` ラッパー。

```text
✓ 91a72bd Fix invoice calculation      koji  2 hours ago
✗ 7b18e20 Update GraphQL schema        koji  5 hours ago
● d81a991 Update README                koji  1 day ago
```

- 実行直後にローカルの Git 履歴を即表示し、CI 状態はプレースホルダー (`⠋`) から
  GitHub API 取得完了時に非同期で埋まる
- フルスクリーン TUI ではなく `git log` に近い使い捨て CLI。Alt Screen は使わず、
  最終表示はターミナル履歴に残る (Bubble Tea をインライン再描画基盤として利用)
- GitHub API 障害・未認証・remote なしでも Git 履歴の表示自体は成立する
  (その場合 CI 欄は `?` / `–`)

設計の一次情報は dotfiles の `issues/git-log-gha-status-wrapper.md`。

## 使い方

```bash
glog                 # 直近 20 件 (既定)
glog -n 5            # 件数指定 (-n5 / --max-count=5 も同じ)
glog --stat          # diffstat 付き (CI 記号はヘッダー行のみ)
glog -p              # patch 付き
glog main            # revision 指定
glog -- src/         # pathspec 指定
glog --cached        # HEAD の CI 状態 + staged diff (ラッパー独自モード)
glog --refresh       # CI キャッシュを無視して再取得
glog --no-cache      # CI キャッシュを読み書きしない
```

## `git log` との意図的な違い

- **全引数への互換は目標にしない。** 上記の allowlist 以外の引数はエラーにして
  `git log` の直接利用を案内する (黙って無視しない)
- **既定の表示件数は 20 件** (`git log` は全履歴)。pager を持たないインライン CLI で
  全履歴を流すのは実用性がなく、CI 一括取得数も表示件数に比例するため。
  全部見たいときは `git log` 同様に負数 (`-n -1`) を渡す
- **pager を挟まない。** 巨大な出力になる `-p` は `-n` 併用を推奨
- `--cached` は `git log` に存在しないラッパー独自モード。staged 変更自体に CI 結果は
  存在しないため「HEAD の CI 状態」であることを表示で明示する

## CI 状態の記号

| 表示 | 意味 |
|---|---|
| `✓` | すべての対象 Check が成功 (skipped 混在は成功扱い) |
| `✗` | 1 つ以上の Check が失敗 |
| `●` | queued / in_progress / pending |
| `⊘` | cancelled / skipped / neutral のみ |
| `–` | Check が存在しない (未 push の SHA を含む) |
| `?` | 未取得・取得不能 |
| `⠋` | 取得中 (TTY のみ) |

## GitHub 連携

- 認証は GitHub CLI (`gh`) へ委譲。独自トークンは保存しない
- GraphQL `statusCheckRollup` で表示対象コミットを 1 リクエストに一括問い合わせ
- remote (upstream → origin) の URL から owner/repo を解決。HTTPS / SSH 両対応。
  GitHub 以外の remote は CI 取得対象外
- 結果は `$XDG_CACHE_HOME/glog/github.com/<owner>/<repo>.json` にキャッシュ
  (未設定時は `~/.cache/glog/`)。TTL は状態別 (success/failure 24h, pending 10s など)

## 出力先による挙動

- stdout が TTY: インライン動的更新 (スピナー → 確定記号)
- stdout がパイプ / リダイレクト: 動的描画用の ANSI カーソル制御を出さず、
  CI 取得完了後に静的な最終結果を 1 回だけ出力する (`glog | grep '✗'` が機能する)

## 開発

```bash
make test   # go test ./...
make lint   # golangci-lint (go run 経由・バージョン固定)
```

実行ファイルは `~/dotfiles/bin/glog` (zsh shim) が初回実行時に自動ビルドする。
ソース更新時も shim が検知して再ビルドするため、手動ビルドは不要。
