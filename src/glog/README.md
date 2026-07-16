# glog

GitHub Actions / GitHub Checks の結果をコミットごとに添える `git log` ラッパー。

```text
✓ commit 91a72bdc0ffee218... (HEAD -> master, origin/master)
Author: koji <koji@example.com>
Date:   Thu Jul 16 19:12:47 2026 +0900

    Fix invoice calculation
```

- 実行直後にローカルの Git 履歴を即表示し、CI 状態はプレースホルダー (`⠋`) から
  GitHub API 取得完了時に非同期で埋まる
- TTY では less 風の対話ブラウズ: カーソル移動で選び、Enter でそのコミットの
  **CI job 一覧を展開**できる。`q` で終了し、最終表示 (展開状態を含む) は
  ターミナル履歴に残る
- GitHub API 障害・未認証・remote なしでも Git 履歴の表示自体は成立する
  (その場合 CI 欄は `?` / `–`)

設計の一次情報は dotfiles の `issues/done/git-log-gha-status-wrapper.md`。
対話ブラウズ (カーソル + 展開) は元 issue の非目標だったが、2026-07-16 の
ユーザー指示で解禁した。

## 使い方

```bash
glog                 # 直近 20 件 (既定、git log 標準形式)
glog --oneline       # コンパクト 1 行形式
glog -n 5            # 件数指定 (-n5 / --max-count=5 も同じ)
glog --stat          # diffstat 付き (CI 記号はヘッダー行のみ)
glog -p              # patch 付き
glog main            # revision 指定
glog -- src/         # pathspec 指定
glog --cached        # HEAD の CI 状態 + staged diff (ラッパー独自モード、静的出力)
glog --no-pager      # 対話ブラウズせず静的出力
glog --refresh       # CI キャッシュを無視して再取得
glog --no-cache      # CI キャッシュを読み書きしない
```

### 対話ブラウズのキー操作 (TTY のみ)

| キー | 動作 |
|---|---|
| `j` / `k` / `↑` / `↓` | コミット移動 |
| `Enter` / `Space` / `l` / `Tab` | CI job 一覧の展開 / 折りたたみ |
| `Ctrl-D` / `Ctrl-U` / `PgDn` / `PgUp` | ページスクロール |
| `g` / `G` | 先頭 / 末尾へ |
| `q` / `Esc` / `Ctrl-C` | 終了 (最終表示は履歴に残る) |

less -F 相当のショートカットあり: 全件キャッシュ済みかつ 1 画面に収まる場合は
ブラウズを開かずそのまま出力して終了する。

展開時、そのコミットの詳細が一括取得に含まれていなければ (キャッシュヒット時)、
その SHA だけオンデマンドで追加取得する。

## `git log` との意図的な違い

- **全引数への互換は目標にしない。** 上記の allowlist 以外の引数はエラーにして
  `git log` の直接利用を案内する (黙って無視しない)
- **既定の表示件数は 20 件** (`git log` は全履歴)。CI 一括取得数が表示件数に
  比例するため。全部見たいときは `git log` 同様に負数 (`-n -1`) を渡す
- **pager は外部の less でなく内蔵の対話ブラウズ。** CI job の展開という
  less にできない操作があるため。挙動 (スクロール / q / 履歴残置) は less -FRX に寄せた
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
- GraphQL `statusCheckRollup` で表示対象コミット (状態 + job 名) を
  1 リクエストに一括問い合わせ
- remote (upstream → origin) の URL から owner/repo を解決。HTTPS / SSH 両対応。
  GitHub 以外の remote は CI 取得対象外
- 集約状態は `$XDG_CACHE_HOME/glog/github.com/<owner>/<repo>.json` にキャッシュ
  (未設定時は `~/.cache/glog/`)。TTL は状態別 (success/failure 24h, pending 10s など)。
  job 一覧はキャッシュせず、展開時に必要ならオンデマンド取得する

## 出力先による挙動

- stdout が TTY: 対話ブラウズ (インライン描画、Alt Screen 不使用)。終了時に
  最終結果を静的出力してターミナル履歴に残す
- stdout がパイプ / リダイレクト、または `--no-pager`: 動的描画用の ANSI カーソル
  制御を出さず、CI 取得完了後に静的な最終結果を 1 回だけ出力する
  (`glog --no-pager` や `glog | grep '✗'` が機能する)

## 開発

```bash
make test   # go test ./...
make lint   # golangci-lint (go run 経由・バージョン固定)
```

実行ファイルは `~/dotfiles/bin/glog` (zsh shim) が初回実行時に自動ビルドする。
ソース更新時も shim が検知して再ビルドするため、手動ビルドは不要。
