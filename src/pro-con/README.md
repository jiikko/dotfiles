# pro-con

PM (producer) と PG (consumer) を分けて Claude Code を並列に回すための TUI。
**設計の正本は issue 415** (`issues/` 配下の `415-design-claude-pm-worker-orchestration.md`)。ここには実装側の事情だけを書く。

## 現状: ハリボテ (模擬データで動く)

```sh
bin/pro-con      # claude は起動しない。模擬の backend (fake) が状態を進める
```

- 1 秒ごとに `Poll` し、fake の模擬時間が 1 分進む (カードが列を進む・PG が質問する・watchdog が停滞を拾う・リソースの順番が回る)
- attach (`a`) は本物の `claude attach` の代わりに `pro-con fake-attach <id>` を `tea.ExecProcess` で起動する。
  画面の明け渡しと戻りは本番と同じ経路を通る

| キー | 動作 |
|---|---|
| tab / shift+tab | repo タブの切り替え (global = 全 repo) |
| n | 新しい依頼 (PM へ)。repo のタブで出すとその repo がスコープになる |
| i | issue の一覧から選んで「これやって」と依頼する (repo のタブならその repo、global なら設定の全 repo。未完了だけ。epic は見出しの下に子)。Enter → 補足 (空でよい) → Enter |
| s | PG (consumer) の一覧を開閉 (詳細と同じく下から生える。担当カード・状態・実行中のコマンド・経過。`claude agents --json` の session と session ID で突き合わせて pid を出す。PG でない session は件数だけ) |
| e | 選択中のカードの issue の md をエディタで開く ($VISUAL → $EDITOR → nvim。`tuikit/editor`) |
| y | 選択中のカードの issue の md のパス (素の値) をクリップボードへ |
| Y | 選択中のカードのタイトルと内容 (repo・状態・issue・依頼の原文・質問。整形した参照) をクリップボードへ。本文は `termsafe.PlainBlock` を通す |
| 1〜6 | そのレーンへ直接 (依頼 / 分解済み / 作業中 / 質問待ち / レビュー / 完了。数字はレーンの見出しの先頭に出る) |
| h / l / ← / → / ctrl+f | 左右のレーンへ (空のレーンにも止まる。フォーカスはレーンとカードの 2 段で、空のレーンではレーンだけに当たる) |
| j / k / ↑ / ↓ / ctrl+n / ctrl+p | 列の中で 1 枚 |
| ctrl+d / ctrl+u / space / f / pgdn / pgup | 半ページ |
| g / G / home / end | 列の先頭 / 末尾 |
| enter | 詳細の開閉 (画面の下端、案内の直上に下から生える) |
| ctrl+r | 新版へ切り替える (ライブアップグレード。「新版あり」のときだけ) |
| q / esc | 開いている板を 1 つ閉じる (session の一覧 → 詳細)。q は何も開いていなければ終了。ctrl+c は即終了 |
| a | PG の session を開く (今は模擬) |
| r | 質問待ちのカードに回答する |
| + | 追加オーダー (tab で 追記 / 方針変更 / 別件)。**方針変更は y/N 確認** (y / enter だけが実行、他のキーは取り消し) |
| ? | btw (PG を止めずに状況を聞く) |

入力欄 (n / r / + / ?) は readline の編集キーが効く (ctrl+h / ctrl+w / ctrl+u / ctrl+k / ctrl+a / ctrl+e / ctrl+b / ctrl+f …。
`tuikit/lineedit`)。入力中は最下行の案内が入力欄のキーに替わる。

**キーの語彙は [`docs/glogx-ui-guide.md`](../../docs/glogx-ui-guide.md) の 3 層 (vim / emacs 別名 / 動作) に従う**。
上下の移動は `tuikit/listnav.MotionOf` に渡す (glogx と同じ語彙を 1 か所で持つ)。画面固有の動作キーは先に捌く。

glogx と意味を変えている字 (`a` attach / `r` 回答 / `n` 新しい依頼 / `s` session の一覧) とその理由はガイドの §8。
`o` (ブラウザで開く) と `b` (半ページ上) はガイドの意味のために空けてあり、追加オーダーは `+`、btw は `?`。

## 設定 (`~/.config/pro-con/config.toml`)

```toml
repo_roots = ["~/src"]      # この直下の git repo を列挙する (深くは掘らない。.git がファイルの worktree も拾う)
repos      = ["~/dotfiles"] # root の外にある repo を個別に足す
```

- ファイルが無ければ上の値が既定 (`$XDG_CONFIG_HOME` があればその下)。**壊れた TOML と知らないキーはエラーで起動しない**
  (書き間違えたキーを黙って無視すると「設定したのに効かない」が無音で起きる)
- 読めない root・repo でない `repos`・同じ名前の repo は警告にして起動する (通知行に件数と 1 件目)
- タブに出るのは「列挙した repo のうち、カードが 1 枚以上ある repo」だけ (~/src の下は数十 repo ある)。
  列挙に無い repo のカードは global タブにだけ出る。repo はディレクトリ名でカードと突き合わせる

## 新しい依頼のスコープ

`n` で出した依頼は、PM に渡す指示の先頭に前置きが付く (`backend.PMPrompt`。本文は書いたまま末尾に置く):

- **repo のタブ**から: 「この依頼のスコープは repo `<名前>` (`<パス>`) の中だけ。外のファイルを読んだり変更したりしない。issue とカードもこの repo に作る」
- **global のタブ**から: 「repo を指定していない。どの repo の作業かを判断し、複数にまたがるなら repo ごとにカードを分ける」

渡した指示の全文はカードの詳細に出る (何を渡したかを後から確かめられるように)。

## ライブアップグレード (ctrl+r)

使いながらソースを直すと、裏でビルドされて「新版あり ctrl+r」が出る。ctrl+r で自分自身を新しいバイナリへ入れ替える
(`syscall.Exec`。PID と端末はそのまま)。タブ・レーン・選択・開いている板・書きかけの入力と、模擬の状態を引き継ぐ。

- **ビルドするかの判定とビルドは `bin/lib/go_autobuild.zsh` (shim) に任せる** (`go_autobuild_spawn_if_stale`。glogx の
  autobuild.go と同じ形)。入力の指紋・`*_test.go` の除外・lock・失敗の記録と TTL は shim が正本で、Go 側に写さない
- 新版の判定は「起動したときのバイナリと、今パスにあるファイルが同じか」(同一性・更新時刻・大きさ)。ビルドの失敗は、
  最後に頼んだビルドより後の `.autobuild.failed` (ログは `src/pro-con/.autobuild.log`)。失敗の後も shim には尋ね続ける
- 勝手に切り替えない (入力中・確認中に入れ替わると、打っている途中の文字が迷子になる)。exec の前に、裏で外部コマンドを
  起こしている処理 (claude agents の取得・shim への問い合わせ) が終わるのを最大 5 秒待つ (glogx issue 211 と同じ理由)
- 引き継ぐ状態は `$XDG_STATE_HOME/pro-con` (既定 `~/.local/state/pro-con`) の `resume-*.json`。正常に終わったら消す。
  新版が異常終了したら残し、再起動の方法 (`PRO_CON_RESUME=<パス> bin/pro-con`) を出す。確認中 (y/N) は引き継がない。
  書きかけの入力は宛先 (カード / タブ) が変わっていたら戻さず、書いた文を消すまで残る行に出す (esc で消す)
- 使えるのは `bin/pro-con` から起動したとき (ソースのディレクトリの中のバイナリ) だけ。それ以外は ctrl+r で理由を出す
- 既知の制約: 旧版へ自動で戻る仕組みは持たない (新版が起動直後に落ちたら、直して起動し直す)。exec の直後にもう一度
  差し替わった新版は、次の差し替えまで「新版あり」にならない (起動時の Stat が、動いている版ではなくパスのファイルなので)

## テスト・lint の実行中

PG がテストや lint を実行している間も、カードは**作業中の列のまま** (列を移るのは担当が変わるときだけ)。バッジに
`▶ make test 3分` (リソースを占有していれば `▶ device: make e2e-device 3分`) を出す。watchdog の停滞の閾値は、
実行中は「見込みの所要の 2 倍」と通常の閾値の長い方 (`card.StallThreshold`。長いテストを停滞と誤判定しない)。

## issue の一覧から依頼する (`i`)

issue の読み方 (状態 = ファイルの位置、`epic/<name>/` の 2 段、`next/` の目印) は **glogx の issues viewer と同じ `glogx/issues`** に任せる
(`FindDirs` / `Scan` / `LoadMeta`。判定を 2 実装にしない)。並びも viewer と同じ。epic の親は「group 名と同じ番号の issue」
(glogx の `issues_view.go` の groupHead と同じ規則)。epic の見出しを選ぶと、未完了の子の一覧を付けた依頼になる。
依頼のカードは最初からその issue に紐づき、PM への指示に issue のパス (epic なら子の一覧) と repo のスコープが入る。

## issue のファイル

カードは issue を repo + 番号で持つ。`e` / `y` は、設定で列挙した repo の issue ディレクトリを `glogx/issues` で読んで番号の md を探す
(状態のディレクトリ・epic の下も探す。`next/` の claim の目印 = symlink は実体へ)。同じ番号が 2 つあれば
黙って選ばずエラー。カードに issue が複数あるときは最初の 1 つ (1 行目に `#415+1` のように残りの数を出す)。

## 構成

| package | 役割 |
|---|---|
| `config` | 設定ファイルと repo の列挙 |
| `agents` | `claude agents --json` で動いている session を一覧する (本物を読む。3 秒ごと・timeout 3 秒。取れなかったら 0 本にせずエラー) |
| `card` | ドメイン (カード・状態・不変条件の検査)。UI にも backend にも依存しない |
| `backend` | UI と状態の持ち主の境界 (`Backend` interface / `Command` / `Snapshot`)。UI はここより下を知らない |
| `fake` | 模擬 backend。本物に差し替えるときは `backend.Backend` を満たす実装を足し、`main.go` の 1 行を替える |
| `ui` | bubbletea v2 の TUI。状態は持たない (Snapshot を描き、Command を送るだけ) |

- `fake` の dispatcher / watchdog / リソース列は**模擬**で、本番の判定ではない。本番の判定を育てるなら fake から切り出す
- **見た目は B「枠」で合意** (2026-09-24。A 帯 / C カードと実物で見比べて選んだ)。列と詳細を角丸の罫線で囲み、選択中のカードがある列の枠だけを
  現在地色 (202) にする。状態の色は `theme/colors.yml` の意味に揃える (番号の手書きコピー。`ui/style.go` の冒頭)
- 表示の文言と配置にはテストを書いていない (テストはキー入力 → Command → 状態遷移のつなぎ込みだけ)
- 列幅の下限は 14 桁なので、**幅 89 桁未満の端末では行が端末幅を溢れる** (見た目を決めるときに列の畳み方と一緒に決める)
- **カードが列を移るときは滑らせる** (`ui/motion.go`。所要 800ms・終わり際に減速。演出の部品は `tuikit/anim` を replace で取り込む)。
  元の場所には点線の枠を残し、移動先の場所は着地まで空けて待つ。移動中のカードは地の色を変えず、周りの枠だけを移動先の状態の色にする
- **カードは固有の地の色を持つ** (`ui/style.go` の `cardColor`。ID から決まるので列を移っても同じ色)。選択中は左右の両端に現在地色の ▌ ▐ を立て、タイトルを太字 + 下線にする (地は塗り替えない。3 案から a で合意)
- 🚨 **移動中も他のカードの位置と枠の高さは変えない**。列の中は「その列に入った順」に並べ (移ってきたカードは末尾に着地する)、
  枠の高さは画面の残りで固定する。`TestOtherCardsStayPutDuringMotion` が検査する
