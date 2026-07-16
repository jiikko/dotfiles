# glog

GitHub Actions / GitHub Checks の結果をコミットごとに添える `git log` ラッパー。

```text
✓ commit 91a72bdc0ffee218da39a3ee5e6b4b0d3255bfef (HEAD -> master, origin/master)
Author: koji <koji@example.com>
Date:   Thu Jul 16 19:12:47 2026 +0900

    Fix invoice calculation

✗ commit 7b18e20aa1b2c3d4e5f60718293a4b5c6d7e8f90
    ✓ build
    ✗ lint          ← Enter で展開した CI job 一覧
Author: koji <koji@example.com>
Date:   Thu Jul 16 14:03:21 2026 +0900

    Update GraphQL schema
```

## 何ができるか

- **履歴は即時、CI は非同期**: 実行直後にローカルの Git 履歴を表示し、CI 状態は
  プレースホルダー (`⠋`) から GitHub API の取得完了時に `✓ ✗ ● ⊘ – ?` へ埋まる
- **less 風の対話ブラウズ** (TTY のみ): `j`/`k` でコミットを選び、`Enter` で
  そのコミットの **CI job 一覧を展開**。`q` で終了し、最終表示 (展開状態を含む)
  はターミナル履歴に残る
- **壊れない**: gh 未導入・未認証・GitHub 以外の remote・API 障害のどれでも
  Git 履歴の表示自体は成立する (CI 欄が `?` / `–` になり、警告 1 行を stderr へ)
- **パイプ安全**: stdout が非 TTY なら ANSI カーソル制御を出さず、取得完了後に
  静的な最終結果を 1 回だけ出力する (`glog --no-pager -n 50 | grep '✗'` が機能する)

## セットアップ

dotfiles 標準構成ならセットアップ不要。`~/dotfiles/bin` が PATH に入っており、
`bin/glog` (zsh shim) が初回実行時に Go バイナリを自動ビルドする。ソース更新時も
shim が検知して再ビルドするため、手動ビルドは不要。

前提:

- `git` / `go` (ビルド用)
- `gh` (GitHub CLI) + `gh auth login` 済み — CI 状態の取得に使う。無くても
  履歴表示は動く

## 使い方

```bash
glog                     # 直近 20 件をブラウズ (git log 標準形式)
glog --oneline           # コンパクト 1 行形式
glog -n 5                # 件数指定 (-n5 / --max-count=5 も同じ、-n -1 で無制限)
glog --stat              # diffstat 付き
glog -p                  # patch 付き (-n 併用を推奨)
glog main..HEAD          # revision 指定
glog -- src/glog/        # pathspec 指定
glog --cached            # HEAD の CI 状態 + staged diff (独自モード、静的出力)
glog --no-pager          # 対話ブラウズせず静的出力
glog --refresh           # CI キャッシュを無視して再取得
glog --no-cache          # CI キャッシュを読み書きしない
glog --help              # ヘルプ (キー操作・記号・終了コードの詳細)
```

### 対話ブラウズのキー操作 (TTY のみ)

コミット一覧:

| キー | 動作 |
|---|---|
| `j` / `k` / `↑` / `↓` / `Ctrl-N` / `Ctrl-P` | カーソル移動 |
| `Enter` / `Space` / `l` / `→` / `Tab` | CI job 一覧のポップアップを開く |
| `p` | **コミットに紐づく PR をブラウザで開く** (`associatedPullRequests` で解決。ブランチ指定は不要。複数あれば OPEN > MERGED 優先。無ければヒント行に通知) |
| `Ctrl-D` / `Ctrl-U` / `PgDn` / `PgUp` | ページスクロール |
| `g` / `G` | 先頭 / 末尾のコミットへ |
| `q` / `Esc` / `Ctrl-C` | 終了 (最終表示は履歴に残る) |

CI job ポップアップ表示中 (開いた直後のフォーカスはタイトル行):

| キー | 動作 |
|---|---|
| `j` / `k` / `↑` / `↓` / `Ctrl-N` / `Ctrl-P` | フォーカス移動 (`j` で job へ降り、`k` でタイトル行へ戻る) |
| `Enter` / `Space` | タイトル行: 閉じる。job: **詳細ポップアップを TUI 内で開く** (Enter は一貫して「TUI 内の開閉 toggle」) |
| `l` / `→` / `Tab` | job: 詳細ポップアップを開く (Enter と同じ) |
| `g` / `G` | 先頭 / 末尾の job へ |
| `o` | **選択中の job の詳細ページをブラウザで開く** |
| `p` | コミットに紐づく PR をブラウザで開く (一覧と同じ) |
| `y` | **URL をクリップボードへコピー** (job 選択中はその job、それ以外はコミット。LLM に貼る用) |
| `h` / `←` / `Esc` / `q` | ポップアップを閉じる (`q` はビューを 1 段戻る tig 流。即終了は `Ctrl-C`) |

### job 詳細ポップアップ (`Enter` / `l`)

job パネルの下に第 2 ポップアップを重ね、その job の「何が起きたか」を表示する:

- **annotations 優先**: CI が報告した `[failure] path:line + メッセージ` の構造化データ
  (`gh api …/check-runs/<id>/annotations`)。エラーの要点が凝縮されていて LLM に渡す素材
  としても最良
- annotations が無ければ**ログ末尾 50 行** (`gh run view --job <id>`)。失敗 job は
  `--log-failed` で**失敗ステップのログのみ**。開いた直後は末尾 (直近の出力) を表示
- 行頭のタイムスタンプは除去 (幅の節約)。ツールが出力した ANSI カラーは保持し、
  `##[error]` / `##[warning]` / `##[group]` 等のマーカーは Web UI 風に glog 側で着色
  (raw ログにこれらの色情報は無いため)
- `j`/`k`/`Ctrl-D`/`Ctrl-U`/`g`/`G` でスクロール、`Enter`/`h`/`Esc`/`q` で job 一覧へ
  戻る (Enter は開閉 toggle)。`o` でブラウザ
- GitHub Actions の job (CheckRun) 限定。外部 CI (StatusContext) はログの取得経路が無い
- 表示行数は端末の高さに自動適応 (低い端末でも末尾スクロールが機能する)
- 取得結果はメモリ内キャッシュ (同じ job の再表示は即時)

`y` のクリップボードコピーは tmux 内なら `tmux load-buffer -w` (tmux バッファ +
OSC52 でシステム側にも届く)、tmux 外は `pbcopy` (macOS) / `xclip`。

ポップアップは対象コミットのヘッダー行直下へ重ねて表示する (リストに行を
差し込まない。インライン展開だと開閉のたびに後続行がずれて高さがガタつくため)。
画面下端で収まらない場合はビューポート内へ収まる位置まで引き上げる。
job 詳細ページの URL は CheckRun の `detailsUrl` (Actions のジョブ画面) /
StatusContext の `targetUrl`。URL が無い job では開かず、その旨をヒント行に出す。

- less -F 相当のショートカット: 全件キャッシュ済みかつ 1 画面に収まる場合は
  ブラウズを開かずそのまま出力して終了する
- 展開時、そのコミットの詳細が手元に無ければ (キャッシュヒット時)、その SHA
  だけオンデマンドで追加取得する。進行中の一括取得に含まれる SHA は結果を待つ
  (重複リクエストは打たない)

### CI 状態の記号

| 表示 | 意味 |
|---|---|
| `✓` | すべての対象 Check が成功 (skipped 混在は成功扱い) |
| `✗` | 1 つ以上の Check が失敗 |
| `●` | queued / in_progress / pending |
| `⊘` | cancelled / skipped / neutral のみ |
| `–` | push 済みだが Check が存在しない |
| `↑` | 未 push (GitHub 上にまだ存在しない) |
| `?` | 未取得・取得不能 (gh 未導入 / 未認証 / API 障害) |
| `⠋` | 取得中 (TTY のみ) |

「Check なし (`–`)」「未 push (`↑`)」「取得失敗 (`?`)」は意図的に区別している。
未 push の判定は `git rev-list --not --remotes` によるローカル判定で、これらの SHA は
GitHub へ問い合わせない (必ず「無い」と返るため。API 消費の節約と、push 直後に
古い「Check なし」キャッシュが当たる混同の防止)。

### 終了コード

| コード | 意味 |
|---|---|
| 0 | Git 履歴の表示に成功。**CI 取得の失敗は警告 1 行に落として 0 を返す** |
| 2 | 引数エラー (未対応の引数を含む) |
| その他 | git 自体の失敗。git の終了コードと stderr をそのまま伝播 |

## `git log` との意図的な違い

- **全引数への互換は目標にしない。** allowlist (上記) 以外の引数はエラーにして
  `git log` の直接利用を案内する。黙って無視すると「効いているつもり」の事故に
  なるため
- **既定の表示件数は 20 件** (`git log` は全履歴)。CI の一括取得数が表示件数に
  比例するため。全部見たいときは git と同じ負数 (`-n -1`) を渡す
- **pager は外部の less でなく内蔵の対話ブラウズ。** 「CI job の展開」という
  less にできない操作があるため。挙動 (スクロール / q / 履歴残置 / -F 相当) は
  less -FRX に寄せた
- `--cached` は `git log` に存在しない独自モード。staged 変更自体に CI 結果は
  存在しないため、「**HEAD の** CI 状態」であることを表示で明示する

## GitHub 連携の仕組み

- **認証は `gh` へ委譲**。独自トークンは保存しない。API 呼び出しは
  `gh api graphql` 経由
- **1 リクエスト一括取得**: GraphQL `statusCheckRollup` で表示対象コミット全件
  (集約状態 + job 名) を SHA ごとの alias で 1 クエリに束ねる。コミットごとの
  REST 逐次呼び出しはしない。上限 100 SHA (超過分は `?`)
- **リポジトリ解決**: 現在ブランチの upstream remote → `origin` の順で remote URL
  から owner/repo を解決。HTTPS / SSH (`git@` / `ssh://`) 両対応。GitHub 以外の
  remote は CI 取得対象外 (CI 欄は `–`)
- **集約ルール** (優先順): 失敗あり → `✗` ＞ 実行中あり → `●` ＞ 成功あり → `✓`
  ＞ cancelled/skipped/neutral のみ → `⊘` ＞ Check なし → `–`

### キャッシュ

`$XDG_CACHE_HOME/glog/github.com/<owner>/<repo>.json` (未設定時は
`~/.cache/glog/`) に集約状態を保存する。CI は再実行されうるため永久キャッシュ
にはせず、状態別 TTL で失効させる:

| 状態 | TTL |
|---|---:|
| success / failure | 24 時間 |
| cancelled / skipped / neutral | 1 時間 |
| pending / in_progress | 10 秒 |
| Check なし | 5 分 |
| 取得失敗 (unknown) | 30 秒 (負キャッシュ。障害中に毎回 10 秒待たない) |

- job 一覧はキャッシュしない (展開時に必要ならオンデマンド取得)
- TTL 切れのエントリは保存時に間引く (最長 TTL が 24h なのでファイルは常に直近
  1 日分程度)。加えてエントリ数の上限 2000 件を超えた分は取得時刻の新しい順に残す
- 書き込みは temp + rename の原子的更新。キャッシュの欠損・破損は「キャッシュ
  なし」として動作し、コマンドを失敗させない

## トラブルシューティング

| 症状 | 原因と対処 |
|---|---|
| CI 欄が全部 `?` + 「gh が見つからない」 | `brew install gh` |
| CI 欄が全部 `?` + 「未認証」 | `gh auth login` |
| CI 欄が全部 `–` | remote が GitHub でない。`git remote -v` を確認 |
| CI 欄が `↑` | 未 push。push すれば次回から取得対象になる |
| 直前に再実行した CI が反映されない | キャッシュ TTL 内。`glog --refresh` |
| rate limit の警告 | しばらく待つ。キャッシュがあるので通常は到達しない |

## 開発

```bash
make test   # go test ./... (unit + 一時 git リポジトリでの integration。外部通信なし)
make lint   # golangci-lint (go run 経由・バージョン固定、設定は .golangci.yml)
```

- CI: `.github/workflows/lint.yml` の `go-lint` ジョブが lint と test を回す
- 実装は flat な `package main`。境界: `options.go` (引数 allowlist) /
  `gitlog.go` (git 実行と %x1e/%x1f レコード解析) / `github.go` (repo 解決・
  GraphQL・集約) / `cache.go` (XDG キャッシュ) / `render.go` (行生成) /
  `tui.go` (Bubble Tea ブラウズ) / `main.go` (配線)
- GitHub API はテストでは `CommandRunner` を fake に差し替える (fixture 駆動)

## 設計メモ

- 設計の一次情報: dotfiles の `issues/done/015-feat-git-log-gha-status-wrapper.md`
- コミット境界の解析は人間向け出力の正規表現ではなく、`--pretty=format:` への
  制御文字 (`%x1e` / `%x1f`) 埋め込みで行う。`--stat` / `-p` の本文を壊さない
- Bubble Tea はフルスクリーン TUI ではなく「非同期レンダリング可能な CLI
  ランタイム」として使う。Alt Screen には切り替えず、インラインのビューポート
  描画。終了時は TUI 領域を消して最終結果を静的出力する
- 対話ブラウズ (カーソル + 展開) は元 issue の非目標だったが、2026-07-16 の
  ユーザー指示で解禁した
- 未対応 (必要になったら issue 化): `--watch` / 失敗 workflow への URL 表示 /
  `--json` / GitHub Enterprise Server / GitHub 以外のホスティング
