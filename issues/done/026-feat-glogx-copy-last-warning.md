# 026 feat(glogx): 直近の警告/エラーテキストを `w` でクリップボードへコピー

## 背景

glogx の主要実行環境は tmux の `display-popup` 内 (`~/.tmux.conf` の `bind g display-popup -E …`)。
display-popup はモーダルなオーバーレイで、**tmux copy-mode に構造的に入れない** (popup は
pane ではなくクライアント側オーバーレイなので `copy-mode` は背後の元ペインに入ってしまう。
tmux 3.7 で検証済み)。そのため popup 内の glogx が出したテキストを copy-mode で選択コピー
することはできない。

一方で glogx は既に `y` (URL コピー) / `Y` (job 詳細コピー) で `copyToClipboard`
(内部 `pbcopy`) を使っており、**これは tmux 非依存でプロセスから直接 macOS クリップボードへ
書くため popup 内でも動く**。同じ仕組みで警告/エラーテキストもコピーできる。

### 直接の動機 (ユーザー要望 2026-07-23)

起動時に `macism` 未導入トースト (`macism 未導入: brew tap laishulu/homebrew && brew install
macism`、tui.go の Init 近辺) が出ることがあり、その **`brew install …` コマンドをコピーして
ターミナルに貼って実行したい**。しかしトーストは数秒で自動消滅する (toast.go の
`toastHold` 後に退場) ため、消えた後にコピーする手段が無い。

### 却下した代替案: nvim で開く

「`w` で警告を nvim に開いて中身をヤンク」も検討したが不採用。理由:
- 開く→ヤンク→終了の手数がかかる
- nvim のヤンクはデフォルトだと system クリップボード (`pbcopy`) に入らない設定が多く
  「コピーしたのに `prefix+]`/`Cmd-V` で貼れない」になりがち
- `pbcopy` 直コピーならキー 1 発でターミナルに貼れる (この用途に最短)

## 設計

### 不変条件

- **直近の警告/エラーは、画面表示が消えた後でもコピー可能であること**。トーストは transient
  なので、表示状態とは別に「直近の警告文字列」を保持する必要がある

### `lastWarning` フィールドの新設 (browseModel)

トーストは数秒で消えるため、`m.toast.text` を読む方式は「消えた後コピーできない」で不変条件を
破る。表示状態と独立に直近の警告文字列を保持する:

```go
// browseModel に追加
lastWarning string // w でコピーする直近の警告/エラー文字列 (表示が消えても保持)
```

保持するのは **失敗系の情報だけ**にする (成功トースト「push しました」等をコピーしても無意味)。
書き込み点は「エラー/警告を出す箇所」に集約する。候補 (grep 済み):

- `m.toast.show(…, false)` の第 2 引数 `false` = 失敗トースト (push/pull/rerun 失敗、
  macism 未導入・切替失敗)。**成功トースト (`true`) は対象外**
- `m.ghErr` セット時の `ghErr.Warning()` (CI 取得の sticky 警告)
- `m.notice` のうちエラー系 ("… に失敗しました" / "開けません" 等)

二重管理を避けるため、`toast.show` をそのまま呼び分けるのではなく **ヘルパー経由に寄せる**のが
望ましい (下記)。

### 推奨: 警告発行を 1 関数に集約 (重複除去)

現状 `m.toast.show(text, false)` が散在している (tui.go に複数箇所)。`w` のコピー対象を
漏れなく捕捉するため、失敗トーストの発行を 1 ヘルパーに通す:

```go
// showWarning は失敗トーストを出しつつ lastWarning に残す (w でコピーできるように)。
// 成功トースト (toast.show(…, true)) はこれを通さない。
func (m *browseModel) showWarning(text string) {
    m.lastWarning = text
    m.toast.show(text, false)
}
```

既存の `m.toast.show(…, false)` 呼び出し (push 失敗 / pull 失敗 / rerun 失敗 / macism
未導入 / macism 切替失敗 = imeWarn) を `m.showWarning(…)` へ置換する。CLAUDE.md「自律改善:
重複コード」に沿う。`ghErr` / エラー系 `notice` も余力があれば同ヘルパー相当で `lastWarning`
を更新する (最低限、直接の動機である macism トーストが確実に入ればよい)。

### `w` キーの追加 (handleKey のコミット一覧ビュー)

`b`(push) / `u`(pull) / `C`(claude update) と同じレイヤー (モーダル・prefix・実行中ガードの
**後**、diff/PR ポップアップのディスパッチと整合する位置) に置く:

```go
if key == "w" {
    if m.lastWarning == "" {
        m.notice = "コピーできる警告はありません"
        return m, nil
    }
    if err := copyToClipboard(m.lastWarning); err != nil {
        m.notice = "コピーに失敗しました: " + firstLine(err.Error())
        return m, nil
    }
    m.notice = "警告をコピーしました"
    return m, nil
}
```

キー選定: `w`。既存の一覧ビューのキー (`j/k/g/G/y/p/P/d/o/b/u/C/U/q/enter/…`) と衝突しない
(grep 済み。`w` は未割当)。`y`(URL)/`Y`(job 詳細) と並ぶ「コピー系」として自然。

### hint 行への追記

最下部ヒント (`hintLine`) の末尾に `w: 警告コピー` 相当を足すか検討する。ただしヒントは既に
長く省略される (`…`) ので、必須ではない (`b: push` 等と同格の扱いでよいか実装時に判断)。

## 実装詳細 (touch points)

| ファイル | 変更 |
|---|---|
| `tui.go` | `browseModel` に `lastWarning string` 追加 / `showWarning` ヘルパー追加 / 既存 `toast.show(…, false)` を `showWarning` へ置換 / `handleKey` に `w` の case 追加 / Init の macism 未導入トーストを `showWarning` 経由に / imeWarn (main.go 側で `browse.toast.show(imeWarn, false)`) も lastWarning に載る経路にする |
| `main.go` | `imeWarn` を toast に出す箇所 (runLog、`browse.toast.show(imeWarn, false)`) を、lastWarning にも残る形へ (browseModel のセッターを 1 つ用意して呼ぶ、または newBrowseModel 前に field 直代入) |

`copyToClipboard` は既存の差し替え点 (external_commands.go、テストで実クリップボードを触らない
`var`) をそのまま使う。

## テスト

- `copyToClipboard` をスタブ差し替え (既存テストと同じ手法) して:
  - 警告トースト発行後に `w` → スタブに `lastWarning` の文字列が渡る / notice が
    「警告をコピーしました」
  - トーストが消えた状態 (phase=hidden) でも `w` でコピーできる (`lastWarning` 保持の回帰。
    **不変条件の核**なので必ず書く)
  - 警告が一度も無い状態で `w` → コピーせず「コピーできる警告はありません」
  - `copyToClipboard` がエラーを返す → notice に理由 (既存 `y` のエラー経路と同型)
- macism 未導入経路: `macismInstalled` スタブを false にして Init 相当を通し、`w` で
  `brew install` を含む文字列がコピーされる (直接の動機の回帰)
- 検証コマンド: `make -C src/glogx lint && make -C src/glogx test` (root からは `make test-src`)

## リスクと判断根拠

- **成功トーストを混ぜない**: `showWarning` を失敗系専用にし、`toast.show(…, true)` は通さない。
  「push しました」をコピーしても無意味で、`lastWarning` が成功文言で上書きされると直前の
  エラーがコピーできなくなる
- **transient と保持の分離**: `lastWarning` は表示ライフサイクル (toast の seq/phase) と独立。
  次の警告が出るまで保持し続ける (クリアしない)。「古い警告が残る」より「消えてコピー不能」を
  避ける方を優先する
- popup 非依存: `pbcopy` 直書きなので display-popup 内でも動く (既存 `y` で実証済み)。
  copy-mode の制約 (本 issue 背景) を回避する正攻法

## スコープ外

- tmux copy-mode を popup 内で使えるようにする件 (tmux の制約で不可。別途 tmux 設定側で
  「別バインドで window 起動」等を検討する話であり、glogx の変更ではない)
- 警告の履歴 (複数保持・一覧表示)。直近 1 件で足りる
- 警告以外 (コミット本文全体など) の汎用コピー。既存 `y`/`Y` の範囲を超える拡張は別 issue
