# glogx

**glog (read-only) のコピーに write 操作と Claude Code 連携を足した派生版。**
push (`b`) / pull --rebase (`u`) に加え、Claude Code の `/usage` 残量表示 (`U`) と
`claude update` (`C`) を持つ。read-only という glog 本体の契約を守るため、write 操作は
こちらに隔離している。

## glog との共通コード分離について (2026-07-19 の判断)

src/glog の完全コピーから出発しており、github.go / render.go / cache.go 等は本家と
重複している。**意図的に共有パッケージへは分離していない**: glogx は活発に改造する
フェーズで、core を切ると変更のたびに境界判断と glog 側の非破壊確認が入り改修速度を
落とすため。また glogx が手に馴染めば glog 本体を退役して一本化する可能性があり、
その場合この重複問題自体が消える。

以下のどちらかが起きたら再評価すること:

- **glog を月単位で使わなくなった** → glog を退役して一本化 (分離不要のまま終わり)
- **同じ修正を glog と glogx の両方に入れる事態が 2 回起きた** → その時点で
  共有パッケージ (例: `src/glog-core`) を抽出する。実際に二重修正した箇所こそが
  「本当に共有すべき core」の証拠になる

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
  そのコミットの **CI job 一覧をポップアップ表示** (job には所要時間を併記)。
  `q` で抜けると表示は消える (git log の pager と同じ。残したいものは `y` で
  URL コピー / `o` でブラウザ / `--no-pager` で静的出力)
- **PR バッジ**: コミット行の末尾に紐づく PR を `#123` で表示 (OPEN=緑 /
  MERGED=マゼンタ / CLOSED=赤)。一括 GraphQL に相乗りするので追加リクエストなし。
  `p` キーのキャッシュにも合流し、バッジが出ているコミットの `p` は即座に開く
- **壊れない**: gh 未導入・未認証・GitHub 以外の remote・API 障害のどれでも
  Git 履歴の表示自体は成立する (CI 欄が `?` / `–` になり、警告 1 行を stderr へ)
- **パイプ安全**: stdout が非 TTY なら ANSI カーソル制御を出さず、取得完了後に
  静的な最終結果を 1 回だけ出力する (`glogx --no-pager -n 50 | grep '✗'` が機能する)
- **write 操作 (glog に無い独自機能)**: `b` で push (y/N 確認)、`u` で pull --rebase
  (conflict は自動 abort で元に戻す。未コミット変更があるときは案内して中止)。push/pull 後は
  実行中の CI をポーリングして結果を反映する。job パネル / job 詳細の `r` で失敗 job を
  再実行 (y/N 確認。`gh run rerun --job`。反映されるまでパネルをポーリングして追従)
- **Claude Code 連携**: `U` で `/usage` の残量を右上モーダルに表示 (1 分ごとに自動更新)、
  `C` で `claude update` を実行 (結果を下部モーダルに表示)
- **tmux popup 対応**: ctrl+g の popup 内では tmux prefix が window 操作に効かないため、
  押すとその旨を案内し、続く 1 キーを無視する (誤爆でコミットを選ばない)

## セットアップ

dotfiles 標準構成ならセットアップ不要。`~/dotfiles/bin` が PATH に入っており、
`bin/glogx` (zsh shim) が初回実行時に Go バイナリを自動ビルドする。ソース更新時も
shim が検知して再ビルドする (`usage/` 等のサブパッケージ変更も含む) ため、手動ビルドは不要。

前提:

- `git` / `go` (ビルド用)
- `gh` (GitHub CLI) + `gh auth login` 済み — CI 状態の取得に使う。無くても
  履歴表示は動く

## 使い方

```bash
glogx                     # 直近 20 件をブラウズ (git log 標準形式)
glogx --oneline           # コンパクト 1 行形式
glogx -n 5                # 件数指定 (-n5 / --max-count=5 も同じ、-n -1 で無制限)
glogx --stat              # diffstat 付き
glogx -p                  # patch 付き (-n 併用を推奨)
glogx main..HEAD          # revision 指定
glogx -- src/glogx/       # pathspec 指定
glogx --cached            # HEAD の CI 状態 + staged diff (独自モード、静的出力)
glogx --no-pager          # 対話ブラウズせず静的出力
glogx --refresh           # CI キャッシュを無視して再取得
glogx --no-cache          # CI キャッシュを読み書きしない
glogx --help              # ヘルプ (キー操作・記号・終了コードの詳細)
```

### 対話ブラウズのキー操作 (TTY のみ)

`Ctrl-F` は全ビューで `→` の別名 (`C-n`/`C-p` = ↓/↑)。本家と異なり `Ctrl-B` の `←` 別名は無く、push は `b` (diff 表示中を除く)。

コミット一覧:

| キー | 動作 |
|---|---|
| `j` / `k` / `↑` / `↓` / `Ctrl-N` / `Ctrl-P` | カーソル移動 |
| `Enter` / `Space` / `l` / `→` / `Tab` | CI job 一覧のポップアップを開く |
| `d` | **コミットの diff をポップアップ表示** (`git show --stat --patch`。コード部分は chroma で拡張子ベースのシンタックスハイライト。ほぼ全画面のモーダルで less 流儀にスクロール: `j`/`k`/`Enter` 行送り・`Space`/`f`/`b`/`C-d`/`C-u` 半ページ・`g`/`G`。末尾では最終行を表示したまま止まる。閉じるのは `q`/`h`/`Esc`/`d`。SHA ごとにセッション内キャッシュ) |
| `o` | **コミットの GitHub ページをブラウザで開く** |
| `p` | **コミットに紐づく PR をブラウザで開く** (`associatedPullRequests` で解決。ブランチ指定は不要。複数あれば OPEN > MERGED 優先。無ければヒント行に通知) |
| `P` | **PR の状態ポップアップ** (state / draft / レビュー承認 / conflict / CI をブラウザなしで確認。`o` でブラウザ・`y` で URL コピー・`P`/`q`/`h` で閉じる。mergeable は GitHub 側の遅延計算中は「計算中」表示) |
| `b` | **git push** (y/N 確認。未 push が無ければ警告のみ。diff 表示中の `b` はスクロール) |
| `u` | **git pull --rebase** (y/N 確認。conflict は自動 abort で元に戻す。未コミット変更があると案内して中止) |
| `U` | **Claude Code の /usage 残量を右上モーダルで表示** (toggle。1 分ごとに自動更新) |
| `C` | **claude update を実行** (確認なし即実行。結果を下部モーダルに表示) |
| `Ctrl-D` / `Ctrl-U` / `PgDn` / `PgUp` | ページスクロール |
| `g` / `G` | 先頭 / 末尾のコミットへ |
| `q` / `Esc` / `Ctrl-C` | 終了 (git log の pager と同じく表示は消える) |

CI job ポップアップ表示中 (開いた直後のフォーカスはタイトル行):

| キー | 動作 |
|---|---|
| `j` / `k` / `↑` / `↓` / `Ctrl-N` / `Ctrl-P` | フォーカス移動 (`j` で job へ降り、`k` でタイトル行へ戻る) |
| `Enter` / `Space` | タイトル行: 閉じる。job: **詳細ポップアップを TUI 内で開く** (Enter は一貫して「TUI 内の開閉 toggle」) |
| `l` / `→` / `Tab` | job: 詳細ポップアップを開く (Enter と同じ) |
| `g` / `G` | 先頭 / 末尾の job へ |
| `o` | **選択中の job の詳細ページをブラウザで開く** |
| `p` | コミットに紐づく PR をブラウザで開く (一覧と同じ) |
| `r` | **選択中の失敗 job を再実行** (y/N 確認。`gh run rerun --job`。GitHub Actions の失敗 job 限定。job 詳細ポップアップ内でも同じ) |
| `y` | **URL をクリップボードへコピー** (job 選択中はその job、それ以外はコミット。LLM に貼る用) |
| `Y` | **選択中 job の詳細を Markdown でコピー** (job 名 / commit / URL のヘッダ + step 一覧 + annotations / ログ末尾。LLM に貼る用。未取得なら取得してからコピー。job 詳細ポップアップ内でも同じ) |
| `h` / `←` / `Esc` / `q` | ポップアップを閉じる (`q` はビューを 1 段戻る tig 流。即終了は `Ctrl-C`) |

### job 詳細ポップアップ (`Enter` / `l`)

job パネルの下に第 2 ポップアップを重ね、その job の「何が起きたか」を表示する。
構成は上から:

- **step 一覧**: 各 step の結論 + 所要時間 (`✗ Bench tmux latency (13s)`)。
  どの step で落ちた / どの step が遅いかが一覧で分かる (best-effort。取れなくても以下は出る)
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
  less にできない操作があるため。挙動 (スクロール / q / 終了時に表示が消える /
  -F 相当のショートカット) は git log の less に寄せた
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
| 直前に再実行した CI が反映されない | キャッシュ TTL 内。`glogx --refresh` |
| rate limit の警告 | しばらく待つ。キャッシュがあるので通常は到達しない |

## 開発

```bash
make test   # go test ./... (unit + 一時 git リポジトリでの integration。外部通信なし)
make lint   # golangci-lint (go run 経由・バージョン固定、設定は .golangci.yml)
```

- CI: `.github/workflows/src_glogx.yml` (paths filter 付きの薄い caller) が再利用 workflow
  `_go-project.yml` を呼び、lint と test を回す (src/glogx を触った push/PR のときだけ起動)
- 実装は flat な `package main` (+ bubbletea 非依存の `usage/` サブパッケージ)。主な境界:
  `options.go` (引数 allowlist) / `gitlog.go` (git 実行と %x1e/%x1f レコード解析) /
  `github.go` (repo 解決・GraphQL・集約) / `cache.go` (XDG キャッシュ) /
  `external_commands.go` (git/tmux/claude/browser/clipboard の外部プロセスラッパー) /
  `terminal.go` (端末サニタイズ) / `render.go` (行生成) / `highlight.go` (diff の
  シンタックスハイライト) / `tui.go` (Bubble Tea ブラウズの中核・状態遷移) /
  `box.go` (browseModel 非依存の枠描画プリミティブ = panel/overlay/centerBox/shadow) /
  各種オーバーレイ・モーダル (`diff_overlay.go` / `job_detail_overlay.go` /
  `usage_overlay.go` / `pr_status_overlay.go` / `action_modal.go` / `toast.go`) /
  `usage/` (Claude Code の /usage 取得・整形。単独コマンドへ切り出し可能) / `main.go` (配線)
- GitHub API はテストでは `CommandRunner` を fake に差し替える (fixture 駆動)
- `tui.go` のテストは機能クラスタで分割: `tui_helpers_test.go` (共有ヘルパー) /
  `tui_nav_test.go` (カーソル/スクロール/アニメ/View) / `tui_panel_test.go` (job パネル/詳細/ETA/CI 取得) /
  `tui_actions_test.go` (push/pull/rerun/update) / `tui_overlay_test.go` (diff/PR 状態/コピー) /
  `box_test.go` (枠描画)

## 設計メモ

- 設計の一次情報: dotfiles の `issues/done/015-feat-git-log-gha-status-wrapper.md`
- コミット境界の解析は人間向け出力の正規表現ではなく、`--pretty=format:` への
  制御文字 (`%x1e` / `%x1f`) 埋め込みで行う。`--stat` / `-p` の本文を壊さない
- 対話ブラウズ (カーソル + 展開) は元 issue の非目標だったが 2026-07-16 の
  ユーザー指示で解禁。さらに元 issue の「Alt Screen 不使用・最終表示を履歴に残す」
  も 2026-07-17 のユーザー指示で上書きし、git log の pager と同じ
  「Alt Screen 上でブラウズ・終了時に表示は消える」へ変更した
- 未対応 (必要になったら issue 化): `--watch` / 失敗 workflow への URL 表示 /
  `--json` / GitHub Enterprise Server / GitHub 以外のホスティング
