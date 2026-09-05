# glogx

**glog (read-only) のコピーに write 操作と Claude Code 連携を足した派生版。**
push (`b`) / pull --rebase (`u`) に加え、Claude Code / codex の残量表示 (`U`) と
`claude update` (`C`) を持つ。read-only という glog 本体の契約を守るため、write 操作は
こちらに隔離している。

## glog との共通コード分離について (2026-07-19 の判断 → 2026-07-22 に決着)

src/glog の完全コピーから出発しており、当時 github.go / render.go / cache.go 等は本家と
重複していた。**意図的に共有パッケージへは分離せず**、「glog を使わなくなったら退役して
一本化 (分離不要のまま終わり) / 同じ修正を両方へ入れる事態が 2 回起きたら core を抽出」の
二択で再評価すると決めていた。結果は前者: **glog は 2026-07-22 に退役済み (`40d4a28`) で、
重複問題ごと消滅した**。glogx は flat な package main を維持する (サブパッケージを切る基準は
「実在する第二消費者」か「明示的な分離要望」— issues/ usage/ subproc/ の前例。termsafe は
`src/termsafe` の独立 module へ出した = doctor module も同じ関門を通すため。issue 228)。

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
- **write 操作 (glog に無い独自機能)**: `s` で status viewer (未コミットの変更を一覧して
  stage / unstage / 変更を捨てる)、`b` で push (y/N 確認)、`u` で pull --rebase
  (conflict は自動 abort で元に戻す。未コミット変更があるときは案内して中止)。job パネル /
  job 詳細の `r` で失敗 job を再実行 (y/N 確認。`gh run rerun --job`)
- **doctor (`D`) — 環境の健全性診断と、掃除候補の削除**: 消してよさそうなディスク占有
  (Xcode DerivedData / 各種キャッシュ / シミュレータランタイム等) と、壊れた launchd 登録を、
  **リスク階級・復元方法つき**で一覧する。ディスクの行は `Space` で選び `d` で削除できる
  (**確認プロンプトを必ず挟む**。`y` 以外はすべて中止)。**`Enter` で中を開くとカーソルが
  対象パスの一覧へ移り、そこでも `Space` を押せる** = エントリ全体でなく**ディレクトリ単位**で
  選べる (行頭の印は `*` = エントリ全体 / `+` = 中の一部)。もう一度 `Enter` で畳んで戻る。
  - **削除の作法はカタログが決める**: 直接消す / **ゴミ箱へ移動** (ユーザーのファイルで
    ありうるもの。空にするまで容量は戻らない) / 専用 CLI を実行する (`go clean -modcache` /
    `xcrun simctl runtime delete`。SIP 配下など `rm` できないもの) / コマンドを表示するだけ
  - **消したことは実測で確認する**: 削除の直前と直後に走査し直し、実際に減った分だけを
    「解放しました」と数える。消えていなければ「未完了」を**成功にも失敗にも畳まずに**出す
    (非同期に消す `simctl` が実際にこの形を返す)
  - **記録が残せないなら削除しない**。何をいつ消したかは `$XDG_CACHE_HOME/glog/doctor-history/`
    に残る。**sudo は実行しない** (必要なものはコマンドを表示するだけ)
  - サービス診断 (launchd) は**表示とコピーのみ**で、停止・削除の経路を持たない。
    同じ検査は CLI (`bin/diskdoctor` / `bin/svcdoctor`) からも叩けるが、**CLI は走査だけで
    削除しない**。設計と不変条件は [src/doctor/README.md](../doctor/README.md) と
    `src/doctor/disk/delete.go` の冒頭が正本
- **実行中の CI を追う**: 一覧に **pending (queued / in_progress) のコミットが 1 つでもあれば
  3 秒周期で状態を取り直す**。パネルを開いているか・push からどれだけ経ったかは問わず、
  決着 (success / failure / neutral) するまで追い、決着したら止まる。push 直後に CI がまだ
  1 つも現れないケースだけは 2 分で打ち切る (workflow を持たない repo で回り続けないため)
- 🚨 **pace 判定 (帯 25/10・超過/先行/適正/余裕/余剰) は `_claude/statusline-command.sh` と
  二重実装**。bash に浮動小数点が無いため shell 側は整数の切り捨てで判定するので、Go 側も
  `paceElapsed` (切り捨て) を通してから判定・表示する。乖離は `usage/pace_drift_test.go` が
  突き合わせる (経過率は 0.1 刻みで総当たり)。**shell だけ変えたときに走らせる経路は
  `tests/claude/test_statusline.sh`** — `go test` のキャッシュは外部ファイルの変更を見ないので
  `-count=1` が必要 (実測 2026-09-01)
- **Claude Code 連携**: `U` で `/usage` の残量を右上モーダルに表示 (codex CLI があれば
  その残量も区切り罫線付きで併記。表示中は 1 分ごとに自動更新。
  非表示のあいだは更新を止め、再表示時に古ければ取り直す)、`C` で `claude update` を実行
  (結果を下部モーダルに表示)。**codex も同様に扱う**: 起動時に npm registry と `codex --version`
  を比較して新バージョンがあればトーストで知らせ、`X` で `codex update` を実行する
  (claude の新バージョン検出 / `C` と完全な鏡像)
- **表示中は外部の変更を自動で追う**: 別ターミナルの commit / rebase、Claude Code、`git pull`
  で履歴が動いたら、その場で読み直す (`.git` の fsnotify イベントで気づき、1 分ポーリングを
  保険にする)。**カーソルが先頭なら pull と同じ演出** (新規コミット行が上から降る)、
  **途中を読んでいるときは見ているコミットが同じ画面行に残る**。件数は右下のトーストで出す
  (ポップアップ・モーダル・job パネル・全画面の viewer を開いている間は見送り、閉じた後の
  観測で反映する)
- **tmux popup 対応**: ctrl+g の popup 内では tmux prefix が window 操作に効かないため、
  押すとその旨を案内する。飲み込むのは prefix キー自体だけで、続くキーは通常の glogx
  操作として処理する (押し間違えた後の打ち直しがそのまま効く)

## セットアップ

dotfiles 標準構成ならセットアップ不要。`~/dotfiles/bin` が PATH に入っており、
`bin/glogx` (zsh shim) が初回実行時に Go バイナリを自動ビルドする。ソース更新時も
shim が検知して再ビルドする (`usage/` 等のサブパッケージ変更も含む) ため、手動ビルドは不要。

再ビルドするかは**ビルド入力の指紋**(各 `.go` / `go.mod` / `go.sum` の パス + mtime + サイズ) で
決める。前回ビルドした指紋を `.autobuild.built` に残し、起動のたびに今の指紋と比べるだけ
(実測 0.66ms)。コミットの有無に関係なく効くので、**編集して起動すればそのまま新版が走る**。
git の tree hash は判定に使わず、「どこから作ったか」を `.autobuild.rev` に記録するだけ
(コミット済みの内容しか見えないため判定には使えない。未コミットなら `+dirty` が付く)。

再ビルドは裏で走り (popup にビルド待ちを見せないため、その回は旧バイナリで起動する)、
起動直後に「新しい glogx をビルド中」とトーストで知らせる。**ビルドが終わると
「新しいバージョンが利用可能です」のダイアログが出て、`r` で確認なしに再起動する**
(`exec` で自分を置き換えるので端末・pid はそのまま。issues viewer を開いていれば
同じ画面から再開する)。`r` 以外のキーを押すとダイアログは閉じ、再起動は次の起動に委ねる。
ただし push / pull / `claude update` の確認中・実行中は、それらが片付くまでダイアログを保留する
(再起動は走行中の処理を巻き添えに殺すうえ、確認の `y/N` とキーが競合するため)。
ビルドが失敗した場合は旧版で継続し、その旨を警告トーストで出す (`src/glogx/.autobuild.log`)。

**`u` (pull --rebase) で自分の新しいソースが降ってきた場合も、その場でビルドを始める**
(起動し直さなくてよい)。判定は起動時とまったく同じ規準を shim (`go_autobuild_spawn_if_stale`)
へ委ねているので、glogx と無関係な repo を pull しても何も起きない。完成後の案内は上と同じ
ダイアログ。

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
glogx --no-frame          # 最外周フレーム (板 + 影) を描かない
glogx --help              # ヘルプ (キー操作・記号・終了コードの詳細)
```

### 最外周フレーム (板)

対話ブラウズでは画面全体を**二重罫線の枠 + 右下ドロップシャドウ**で包み、ターミナルの地色の上に
板が浮いているように描く (issue 025)。罫線は**マゼンタ 201** — tmux の scratch popup 枠と同じ
「点滅/scratch アイデンティティ」色で、glogx も「ふだんの pane とは別の一時的な板」であることを
色で示している (色の意味は [`docs/theme-colors.md`](../../docs/theme-colors.md)、値の出典は
[`theme/colors.yml`](../../theme/colors.yml) の `blink_magenta`)。落ち影は色を持たせず中立のまま
(影が主張しないように)。

起動と終了には**中央から開く / 中央へ吸い込まれる**演出が入る (片道 320ms。ユーザー要望 2026-08-01)。
枠を中央から広げ、その中に実画面の左上部分を切り出して入れるので、最初のフレームから 1 行目のコミットが見え、開き切った姿がそのまま本物になる。
終了はこの演出を挟んでから抜けるが、途中でキーを押せば即着地する。`Ctrl-C` は演出なしで即終了する。
枠を持たない画面 (`--no-frame` / 小さい端末) では演出も枠なしで動く。

滑らかさは 3 つで決まる。端末は**文字セル単位でしか動けない**ので、40 行の画面が開き切るまでに
36 行ぶん育つ = フレームが少ないと 1 フレームで何行も跳ぶ:

| 要素 | 効き方 |
|---|---|
| フレーム周期 (`zoomInterval` 16ms) | この演出だけ 60fps。12.5fps だと中間フレームが 2 枚しか出ず点滅に見えた |
| 所要 (`appZoomDuration` 320ms) | 60fps での 1 フレームあたりの平均: 220ms = 2.7 行 / 320ms = 1.8 行 / 420ms = 1.3 行 |
| 曲線の終点 (`scale` の `* appZoomSnap`) | 素の easeOutCubic は進捗 69% で snap に達し、残り 31% は絵が変わらなかった |

- `--no-frame` で無効化できる
- 端末が下限サイズ (60×15) 未満のときは自動で OFF (tmux の小 pane / popup でも崩れない)
- `NO_COLOR` / 非 TTY では色を出さない (枠の字形だけ残る)
- 色を変えたいときは `render.go` の `ansiFrameBorder` を差し替える。候補の見比べは
  `./tools/border-preview.sh` (実端末で実行)

### 対話ブラウズのキー操作 (TTY のみ)

`Ctrl-F` は全ビューで `→` の別名 (`C-n`/`C-p` = ↓/↑)。本家と異なり `Ctrl-B` の `←` 別名は無く、push は `b` (diff 表示中を除く)。

コミット一覧:

| キー | 動作 |
|---|---|
| `j` / `k` / `↑` / `↓` / `Ctrl-N` / `Ctrl-P` | カーソル移動 |
| `Enter` / `Space` / `l` / `→` / `Tab` | CI job 一覧のポップアップを開く |
| `d` | **コミットの diff をポップアップ表示** (`git show --stat --patch`。コード部分は chroma で拡張子ベースのシンタックスハイライト。ほぼ全画面のモーダルで less 流儀にスクロール: `j`/`k`/`Enter` 行送り・`Space`/`f`/`b`/`C-d`/`C-u` 半ページ・`g`/`G`。末尾では最終行を表示したまま止まる。**`J`/`K` (または `shift+↓`/`shift+↑`) で開いたまま次・前のコミットへ差し替える** (一覧のカーソルも追従。端では止まる)。閉じるのは `q`/`h`/`Esc`/`d`。SHA ごとにセッション内キャッシュ) |
| `o` | **コミットの GitHub ページをブラウザで開く** |
| `e` | **nvim を repo root で開く** (`nvim .` なので oil/netrw がそのまま入口になる。閉じると glogx へ復帰。C-g の tmux popup 内でもその popup の中で開く) |
| `E` | **ファイラーを repo root で開く** (yazi → ranger → lf → nnn → vifm の先勝ち。1 つも無ければトーストで案内) |
| `p` | **コミットに紐づく PR をブラウザで開く** (`associatedPullRequests` で解決。ブランチ指定は不要。複数あれば OPEN > MERGED 優先。無ければヒント行に通知) |
| `P` | **PR の状態ポップアップ** (state / draft / レビュー承認 / conflict / CI をブラウザなしで確認。`o` でブラウザ・`y` で URL コピー・`P`/`q`/`h` で閉じる。mergeable は GitHub 側の遅延計算中は「計算中」表示) |
| `b` | **git push** (y/N 確認。未 push が無ければ警告のみ。diff 表示中の `b` はスクロール) |
| `u` | **git pull --rebase** (y/N 確認。conflict は自動 abort で元に戻す。未コミット変更があると案内して中止) |
| `i` | **issues viewer を全画面で開く** (toggle。開くとき右から左へ流し込む演出。repo 直下と `<sub>/issues` の `*.md` をファイル名のカテゴリ別タブで一覧し、カテゴリごとに色を振る (`bug` は赤・`feat` は緑・未知の語は語のハッシュで安定に割る)。`Tab`/`h`/`l` でタブ、`Enter` で本文を markdown 整形して表示 (もう一度 `Enter` で閉じる toggle。右から飛び出す引き出し。画面の 8 割を占め、左に一覧の先頭が残る。閉じるときは逆再生。**本文を開いたまま `J`/`K` (または `shift+↓`/`shift+↑`) で次・前の issue へ差し替える** (親行は飛ばし、端では止まる。演出は挟まず左のカーソルが追従する)。**左に `.md` のソース行番号を出す** — 表示行の連番ではないので nvim や Claude Code へそのまま持ち出せる)、`p` で issue 番号・`y` でパス・`Y` で参照 (番号+タイトル+パス)・`N` で次に採番すべき番号をコピー (**`shift+↑`/`shift+↓` または `K`/`J` で範囲選択すると `y`/`p`/`Y` は選択ぶんをまとめてコピーする**。`Esc` で解除)、**`n` で「次にやる」の目印を付ける / 外す (toggle)** (確認モーダルで `y`/`Enter`。`issues/next/` へ移動する。無ければ作る。目印つきに `n` を押すと `issues` 直下へ戻す。バッジは `▶` で状態フィルタに関係なく常に出る。**タブ行の左端に固定の疑似カテゴリ `[next]`** から一覧できる)、`a` で表示する状態を巡回 (既定は open のみ → +pending → +done)、**`/` で番号のインクリメンタルフィルタ** (`41` と打つと `415`/`141`/`041` が並ぶ部分一致。**タブと状態フィルタの両方を無視して全 issue から探す**ので、既定で伏せている `done` の issue にも番号で飛べる。`Enter` は入力を終えるだけで絞り込みは残るため `y`/`p`/`n` が結果に効く。`Esc` で解除)、本文で `u` を押すと URL 一覧のピッカーが出て、打った文字で絞り込み (`ctrl+n`/`ctrl+p` で移動・`Enter` で開く・`Esc` で戻る)、`e` (別名 `v`) で `$VISUAL`/`$EDITOR` (未設定なら nvim) で開く (🚨 **編集完了まで戻らないエディタを指定すること**。GUI エディタは `code -w` のように 待たせる。git の `GIT_EDITOR` と同じ要求で、待たないと復帰時の取り直しが編集前に走る — ただし viewer を開いている間は下記の自動反映が拾うので、壊れるのではなく反映が遅れる)、`r` で再読込、`s` で status viewer へ切り替え、**`R` で ratelimit ダッシュボードへ切り替え** (一覧・本文のどちらからも。hint には幅の都合で出していない)、`i` で閉じて一覧へ戻る・**`q`/`Esc` は glogx ごと終了** (git log へは戻らない。ユーザー選定 2026-08-06) (**閉じるときは板が 1 枚まるごと等速で右へ抜ける** — 逆再生にすると動き出しが 0.4 秒遅れ、抜けた後に白い画面が残る。途中のキーは即着地させるが飲み込まないので、`q` を押した直後の `q` も効く)。**開いている間は別プロセス (Claude Code / 別ターミナルの $EDITOR / `git pull`) の編集を自動で反映する** (fsnotify のイベントで起こし、mtime + サイズの指紋で判定。カーソル・タブ・スクロール位置は保たれる)。**viewer を出したまま `C-g` で glogx ごと閉じると、次の起動は同じ画面から再開する** (タブ・状態フィルタ・カーソル・開いていた本文とそのスクロール位置。30 分以内・同じ repo のときだけで、演出は出さない。git log 一覧から閉じたときは記憶しない)。状態は `pending/` `done/` などディレクトリで判定し、件数 0 のカテゴリタブは右へ寄せる。規約は [docs/issues-viewer-spec.md](../../docs/issues-viewer-spec.md)) |
| `s` | **status viewer を全画面で開く** (toggle。板が左端から生えてくる演出 — issues viewer が右から流し込むのに対し方向で見分ける)。未コミットの変更を **Staged / Unstaged / Untracked** の 3 区画で一覧し、幅が足りれば右カラムにカーソル行の diff をプレビューする (狭い端末では一覧だけの 1 カラム)。`Space` で **stage / unstage** (向きはセクションが決めるのでキーは 1 つ)、`a` で unstaged + untracked をまとめて stage、`X` で **変更を捨てる** (y/N 確認。untracked は削除。確認に出した時点の状態と実行時の状態が違えば中止する)、`d` で全画面 diff (**`J`/`K` で開いたまま次・前のファイルへ差し替える**。端では止まる)、`Tab` でセクション移動、`r` で再読込、`i` で issues viewer へ切り替え、**`R` で ratelimit ダッシュボードへ切り替え**、`s` で閉じて一覧へ戻る。**`q`/`Esc` は glogx ごと終了** (git log へは戻らない。ユーザー選定 2026-08-06)。`u` は unstage に割り当てていない (`u` = pull の筋肉記憶を守るため)。`p` で **pull --rebase** (y/N 確認は viewer の上に重なる。成功後はその場で読み直す)。`b` で **push** (y/N 確認。一覧と同じキー。成功後はその場で読み直してヘッダーの ahead を消す。当初は「staging の途中から remote 操作へ滑る導線を作らない」として遮断していたが、pull を開けたのと同じ理由で開けた — ユーザー要望 2026-08-07)。**開いている間は別プロセスの編集を自動で反映する** (1.5 秒周期で `git status` を読み直す)。**最下行の案内は端末幅に応じてキーを落とす** (狭くても `s`/`q` の抜ける手段は残る。全キーはこの表と `--help` が正本)。規約は [docs/status-viewer-spec.md](../../docs/status-viewer-spec.md)) |
| `U` | **Claude Code / codex の残量を右上モーダルで表示** (toggle。表示中は 1 分ごとに自動更新。非表示中は更新を止め、再表示時に古ければ取り直す。**issues viewer を開いている間も効く** — viewer の窓に重ねて出る) |
| `R` | **Claude Code / codex の残量を全画面ダッシュボードで表示** (toggle。枠 (5h / weekly) ごとに 1 枚のアナログ盤を並べる。**CLI ごとに罫線で仕切り**、幅が足りれば見出しを 1 段に合体させる (`5H セッション | CLAUDE CODE v… | 7D weekly` — CLI 名の AA と枠ラベルの AA が同じ高さに並ぶ)。幅が足りなければ罫線の中に CLI 名を入れ (`── codex v0.144.6 ────`)、枠ラベルは各盤の真上に出す。**形は画面の中で 1 つに揃える** (段ごとに独立して決めると、CLI 名の桁数の差で「codex の段だけ見出しが大きい」という非対称になる)。盤の下は、**幅が足りれば 1 行に集約する** (ゲージ・使用率・想定と乖離・復活まで・ペース。空いた行は盤に回る — 実測 225x53 で盤 34 行 → 40 行)。🚨 入らない幅では畳まない (情報を落としてまで盤を大きくしない)。**1 周 = その枠の長さ / 12 時 = リセット点 / 針 = いまの経過位置 / 外周の明るい弧 = 復活までの残り時間 / 内周の弧 = 消費した割合**。経過も消費も「窓に対する割合」= 同じ目盛りに乗るので、弧が針より先なら前借り・手前なら余裕が図形だけで読める。盤の下に想定率と乖離 pt・状態語 (適正/先行/超過/余裕/余剰) を出す。狭い端末では 1 カラム → テキストカードへ段階的に落ちる。取得経路は `U` と共用で 1 分ごとに自動更新。`r` で今すぐ更新・**`i`/`s` で issues / status viewer へ切り替え** (viewer 側の `R` と対で往復できる。`U` と違い重ねずに画面ごと入れ替える)・`R`/`q`/`Esc`/`h` で閉じる) |
| `D` | **doctor を全画面で開く** (toggle。掃除候補のディスク占有・壊れた launchd 登録・Homebrew の警告・Docker の未使用資源を、リスク・復元方法つきでタブで並べる。**Docker タブは Docker Desktop がある環境にだけ出る**。停止コンテナ / 使われていないイメージ / ビルドキャッシュ / 参照されていないボリュームを数え、`Enter` で内訳と回収する `docker ... prune` コマンドを見せ、`Space` で選んで `x` で実行する (確認モーダルを必ず挟む。実行の機構は Homebrew タブと共有)。🚨 **ボリュームだけは実行できない** — 中身はデータで消すと戻らず、しかも「最後にマウントされた日」を Docker が持っていないので判定が近似だから。`y` でコマンドをコピーして人が叩く (群のまとめコマンドも出さず 1 件ずつ)。🚨 提示するコマンドには `-f` が付く: prune は対話プロンプトを出し、TTY 無しの実行では**何も消さずに rc=0 で返る**ため (実測)。表示・コピー・実行で同じ文字列を使う (画面に出ているものと走るものを分けない)。見出しの数字は `docker system df` の申告で、候補の合計は共有レイヤーを重複計上する「見積もり」として別に出す)。画面内のキーは `j`/`k` 移動・`Space` で削除するものを選択・`d` で削除 (確認プロンプトを必ず挟む)・`Enter` で中を開く/畳む (開くとカーソルが対象パスへ移り、ディレクトリ単位で選べる)・`y` でパスをコピー・`Y` で解説をコピー・`r` で再スキャン・`D`/`q`/`Esc` で閉じる。**削除できるのはディスクの行だけ**で、サービス診断は表示とコピーのみ (削除経路を持たない)。`risk: 要確認` の行は `Enter` で中身を見るまで選べず、ゴミ箱へ移動する (空にするまで容量は戻らない)。同じ検査は CLI からも叩ける (`bin/diskdoctor` / `bin/svcdoctor` はどちらも走査のみで削除しない。使い分けは [src/doctor/README.md](../doctor/README.md)) |
| `C` | **claude update を実行** (確認なし即実行。結果を下部モーダルに表示。**どの画面からでも押せる** — issues / status / ratelimit / doctor / diff / PR status を開いたままでよく、一覧へ戻る必要はない。viewer が入力中 (絞り込み・y/N 確認) のときだけそちらへ渡す) |
| `X` | **codex update を実行** (`C` の codex 版。起動時に新バージョンの有無を調べ、最新なら「すでに最新」を出す。`C` と同じくどの画面からでも押せるが、**status viewer では `X` = 変更を捨てる**なので、そこでは codex update にならない) |
| `w` | **直近の警告/エラーをクリップボードへコピー** (トーストが消えた後も可。LLM に貼る用) |
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

キー: `j`/`k`/`Ctrl-D`/`Ctrl-U`/`g`/`G` スクロール / **`J`/`K` 開いたまま次・前の job へ** (端では止まる) / `o` ブラウザ / `y` URL コピー /
`Y` 詳細を Markdown でコピー / `r` 失敗 job を再実行 / **`v` 表示中のログを `$VISUAL`・`$EDITOR`
で開く** (既定 nvim) / `Enter`・`h`・`←`・`Esc`・`q` 閉じる。

job パネルの下に第 2 ポップアップを重ね、その job の「何が起きたか」を表示する。
構成は上から:

- **step 一覧**: 各 step の結論 + 所要時間 (`✗ Bench tmux latency (13s)`)。
  どの step で落ちた / どの step が遅いかが一覧で分かる (best-effort。取れなくても以下は出る)
- **annotations 優先**: CI が報告した `[failure] path:line + メッセージ` の構造化データ
  (`gh api …/check-runs/<id>/annotations`)。エラーの要点が凝縮されていて LLM に渡す素材
  としても最良
- annotations が無ければ**ログ末尾 50 行** (`gh api …/actions/jobs/<id>/logs`)。開いた直後は
  末尾 (直近の出力) を表示。失敗 job だけは `gh run view --job <id> --log-failed` で
  **失敗ステップのログのみ**に絞る (この絞り込みは REST の job ログ単体では代替できない)
- **取得は 3 本並列**: step 一覧 / annotations / ログを同時に投げる。annotations の有無は
  job の結果とほぼ 1:1 (実測: success 10/10 が annotations 0 件、failure 12/12 が 1〜4 件)
  なので、非失敗 job では「annotations を見てからログを取る」直列 2 往復目が事実上必ず
  発生していた。ログを投機的に並列化し、annotations が取れた稀ケースでは投機結果を捨てる。
  `gh run view --log` は run 全体のログ zip を落とすため 1 job の 50 行に固定 ~1.0s 余分に
  かかり、REST の job ログ単体へ替えた (合わせて 2.69s → 1.17s)
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
- **一括取得 (並列チャンク)**: GraphQL `statusCheckRollup` で表示対象コミット
  (集約状態 + job 名) を SHA ごとの alias で束ねる。コミットごとの REST 逐次呼び出しは
  しない。上限 100 SHA (超過分は `?`)。レイテンシは SHA 数に線形 (実測: 固定費 ≈ 480ms +
  約 21ms/SHA) なので、件数が多いときは最大 4 本のチャンクへ割って並列に投げる
  (`-n 50` の静的出力で 1.53s → 0.85s)。チャンク 1 は表示順の先頭なので、対話ブラウズでは
  **画面に映っているコミットの CI が最初に埋まる**。1 チャンクが失敗しても取れた分は表示し、
  失敗チャンクの SHA だけが `?` に落ちる
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
- 同じディレクトリに repo 非依存のキャッシュも置く: `claude-latest-version.json`
  (最新版チェック、TTL 1 時間) と `claude-usage.json` (`/usage` + codex の残量、TTL 1 分)。
  `claude -p /usage` は 1 回 ≈ 2.0s wall / 1.8s CPU かかる (トークン課金は無いが node 起動 +
  セッション初期化が重い) ので、起動のたびには払わずキャッシュを使う。定期リフレッシュ側は
  鮮度を作るのが役目なのでキャッシュを読まない

## トラブルシューティング

| 症状 | 原因と対処 |
|---|---|
| CI 欄が全部 `?` + 「gh が見つからない」 | `brew install gh` |
| CI 欄が全部 `?` + 「未認証」 | `gh auth login` |
| CI 欄が全部 `–` | remote が GitHub でない。`git remote -v` を確認 |
| CI 欄が `↑` | 未 push。push すれば次回から取得対象になる |
| 直前に再実行した CI が反映されない | キャッシュ TTL 内。`glogx --refresh` |
| rate limit の警告 | しばらく待つ。キャッシュがあるので通常は到達しない |
| コミットメッセージの絵文字が単色になる | 仕様 (下記「絵文字の幅と脱色」) |
| 絵文字を含む行が再描画のたびに 1↔2 桁ずれる | 対策を足す前に `go run ./tools/width-probe` を実端末で (tmux の内と外の両方で) 走らせて、どの層が幅を割っているかを測ること |

### 絵文字の幅と脱色

表示に出る外部由来テキスト (commit message / CI ログ / job 名) からは **VS16 (U+FE0F) を除去**
している。VS16 付きの字は幅の解釈が層ごとに割れる (x/ansi と tmux は 2、runewidth は 1) 一方、
bare 記号なら全層で 1 に一致するため。割れる文字を出すと、毎フレームの再描画でカラム位置が
ずれて行が揺れる。

**副作用として絵文字は脱色する** (カラー絵文字 → 端末フォントの単色グリフ)。VS16 は幅だけでなく
「カラー絵文字として描け」の指示でもあるため。幅の安定と引き換えに受け入れている意図的な
トレードオフで、色を戻したいなら VS16 を戻す (揺れが再発する) のではなく Unicode Core Mode
(DEC 2027) で幅計算をエンドツーエンドに揃える方向で検討する。詳細と実測値は
`termsafe.DropEmojiVS16` の doc コメント。

この問題は過去に「端末の幅解釈」を測らずに対策を重ねて revert した試行がある (3c74ddf →
3e5787d)。再発時は必ず `tools/width-probe` で測ってから動くこと。

## 開発

```bash
make test   # go test ./... (unit + 一時 git リポジトリでの integration。外部通信なし)
make lint   # golangci-lint (go run 経由・バージョン固定、設定は .golangci.yml)

# 幅ズレ調査用 (要 TTY。tmux の内と外で走らせて比べる)
go run ./tools/width-probe

# 1 フレームの描画コスト観測 (CI では回さない)
go test -run '^$' -bench BenchmarkView -benchmem .
```

🚨 **lint の確認は必ず `make lint` で行う。PATH の `golangci-lint` を直接叩かないこと。**
`make lint` は `go run ...@v2.5.0` で版を固定していて、PATH のバイナリとは指摘が食い違う
(実測 2026-09-01: 同じツリーで `make lint` = 0 issues / PATH の v2.12.2 = 6 issues)。
逆向きも起きる — PATH 版が 0 issues なのに固定版が prealloc を 1 件出し、master の lint が
落ちた実例が `1b025b8`。CI が回すのは固定版なので、そちらが唯一の出典。

- CI: `.github/workflows/src_glogx.yml` (paths filter 付きの薄い caller) が再利用 workflow
  `_go-project.yml` を呼び、lint と test を回す (src/glogx を触った push/PR のときだけ起動)
- Bubble Tea は v2 (`charm.land/bubbletea/v2`)。移行で変えた点・採らなかった v2 機能・上げるときに
  測り直すこと (幅モデルの一致) は [`docs/glogx-bubbletea-v2.md`](../../docs/glogx-bubbletea-v2.md)
- 実装は flat な `package main` (+ bubbletea 非依存の `usage/` / `subproc/` サブパッケージ)。主な境界:
  `options.go` (引数 allowlist) / `gitlog.go` (git 実行と %x1e/%x1f レコード解析) /
  `github.go` (repo 解決・GraphQL・集約) / `cache.go` (XDG キャッシュ) /
  `external_commands.go` (git/tmux/claude/browser/clipboard の外部プロセスラッパー) /
  `terminal.go` (端末サニタイズ) / `render.go` (行生成) / `highlight.go` (diff の
  シンタックスハイライト) / `tui.go` (Bubble Tea ブラウズの中核・状態遷移) /
  `box.go` (browseModel 非依存の枠描画プリミティブ = panel/overlay/centerBox/shadow) /
  各種オーバーレイ・モーダル (`diff_overlay.go` / `job_detail_overlay.go` /
  `usage_overlay.go` / `pr_status_overlay.go` / `action_modal.go` / `toast.go` = 右下の通知
  スタック。新しい通知は上に積まれ古い通知は下から抜ける (最大 3 枚)) /
  `usage/` (Claude Code の /usage と codex rateLimits の取得・整形。単独コマンドへ切り出し可能) /
  `subproc/` (外部プロセス実行の安全弁 = WaitDelay と git の timeout。main / issues / usage の
  3 つが外部コマンドを起動するので、値を main に置くと下位から呼べず写しになる。
  **新しい外部コマンド実行は `subproc.CommandContext` を使う** — 素の `exec.CommandContext` は
  `waitdelay_discipline_test.go` が落とす) /
  `termwidth/` (表示幅の単一情報源 = ansi.StringWidth への一本化。main は `width.go` の別名経由、issues / usage は直接) /
  `sgr/` (基本 ANSI 色。3 パッケージで別名の写しになっていたものを 1 箇所へ) / `main.go` (配線)
- `tools/width-probe/`: 端末が各文字に何セル割り当てるかを CPR (CSI 6n) で端末自身に
  問い合わせる調査ツール。幅ズレの原因層 (glogx / 描画エンジン / tmux / 端末) を推測でなく
  実測で切り分けるためのもので、本体からは参照しない。
  **3 つの幅モデルを並べて出す** (`grapheme` = `ansi.StringWidth` = glogx の `dispWidth` /
  `wc` = `ansi.StringWidthWc` = bubbletea v2 描画エンジンの既定 / `uniseg` = 分割に使っている
  ライブラリ) ので、「端末がどれと一致するか」= **揃える先**が 1 回の実行で決まる (issue 124)。
  ⚠️ **絵文字を「無情報」として外さないこと**: `⚠+VS16` は (grapheme 2 / wc 1 / uniseg 2) で、
  実は一覧中で最も決定力のある probe (実測 2026-08-28)。逆に `🇯🇵` は現在の go-runewidth では
  3 モデル一致で無情報 — 「食い違うのは国旗」はもう成り立たない。
  🚨 「grapheme と wc が割れる」形を残すのが要点で、インド系 (`ಕಾ` `का` `கா`) / Arabic format /
  RI 単独 / keycap がそれ。`U+09BE` や `x+U+0897` は uniseg だけが違う (= 分割器の問題。
  issue 124 の (1)) ので、揃える先の決定には効かない。
  🚨 **`wc` 列は固定の座標系ではない**: `ansi.StringWidthWc` は `mattn/go-runewidth` を使い、
  その版で答えが変わる (`ಕಾ` の wc は v0.0.23 で 1、v0.0.27 で 2)。indirect 依存なので無関係な
  更新で判定が反転しうる。ツールは解決版を出力に載せるので、**結果を残すときは必ず一緒に控える**
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
- **起動は fork の並列化で律速を潰している**: git の 1 fork ≈ 6ms で、直列に積むと初回描画が
  数十 ms 遅れる。`git log` (表示用と解析用の 2 本) / repo 解決 + 未 push 判定を同時に走らせ、
  最長チェーンまで縮める。IME 操作は TIS を直接呼ぶため fork しない
- **対話ブラウズ中は IME を英数へ切り替える** (キー操作が主なため。終了時に元へ戻す)。
  現在ソースの取得・英数への切替・終了時の復元はすべて macOS の TIS を直接呼び出し、
  TIS が返す現在ソース ID で反映を確認する。外部 CLI への依存はない。
  🚨 切替の「実行」は TUI 開始前に完了させる必要がある — raw mode でも IME は OS の入力ソース層で
  効くため、未完了だと打鍵が日本語 IME の composition に吸われる。よって問い合わせ (1 本目) だけを
  先に取得し、切替は TUI 開始直前に行う
- **表示中の追従は「イベントで起こし、指紋で判定する」** (`gitlog_watch.go`。issues viewer の
  見張りと同じ方式)。イベントは真偽の正本にしない — git は 1 操作で index / refs / logs / `*.lock`
  を続けて書き、rebase では HEAD が何度も動くため。起こされたら**表示と同じ revs / max-count /
  paths のまま `--stat` / `-p` を外した `git log` (`%H` + `%D`) を測り**、変わったときだけ読み直す
  (本文は SHA で決まるので、空振りの再読込 = CI の再取得を出さない)。1 分ポーリングは
  イベントの取り落ちと fsnotify を作れない環境のための保険
- 未対応 (必要になったら issue 化): 失敗 workflow への URL 表示 /
  `--json` / GitHub Enterprise Server / GitHub 以外のホスティング
