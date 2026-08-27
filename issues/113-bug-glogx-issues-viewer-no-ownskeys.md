# 113 bug: issues viewer に `ownsKeys()` が無く、外側の `U` 横取りが URL ピッカーの契約を破る

起票日: 2026-08-27 / 出典: leaky-abstraction 監査 (L4 capability の嘘) / priority: medium

## 事実

`src/glogx/url_picker.go:urlPicker.handleKey` の doc は明示的にこう宣言している:

> ⚠️ 印字文字はすべて検索語に流す (default 節)。**ここで個別のキーを先に横取りすると、
> その文字を含む URL を検索できなくなる。**

ところが `src/glogx/tui.go` の `handleKey` は、`if m.issuesOv.visible()` の中で
**viewer へ委譲する前に** `if key == "U" { return m, m.toggleUsage() }` を無条件で実行する
(委譲はその 3 行あと)。つまり URL ピッカーに `U` は永久に届かない。

## 非対称

同型の横取りは status viewer 側にもあったが、そちらは `a9f3fa5` で
`statusView.ownsKeys()` を新設して塞がれている (`v.pagerKey != "" || v.discarding`)。
`tui.go` はそれを `if !m.statusOv.ownsKeys()` でガードしている。

**`ownsKeys` の実装は repo 全体で statusView の 1 つだけ** (grep で確認)。
**issues viewer には対の述語が無く、横断確認もされていない。**

## 発火条件と壊れ方

issues viewer の本文で `u` → URL ピッカー → **大文字 `U` を含む URL**
(`github.com/Ueno/...`、クエリ文字列、base64 断片) を絞り込もうとしたとき。
押した文字が検索語に入らず、代わりに残量モーダルが開く。

**silent**。compile error にも lint にもならない。

## 反証の試み (監査が実施済み)

- `docs/issues-viewer-spec.md` の「viewer の上でも `U` は効く」(ユーザー要望 2026-08-01) は
  **横取りすること自体**の根拠であり、**ピッカー入力中も横取りする**とは書いていない。
  同 spec の URL ピッカー節は「打った文字がそのまま絞り込み」と書いており、2 節が突き合わされていない
- `issues/done/071-*.md` の「反証で崩れた」表に本件は無い

## 将来さらに広がる

同 spec は「タイトル検索は未実装。足すときは検索語へ流す形になる」と予告している。
実装した日に `/` の入力中も同じ横取りに当たる。

## 対応

`issuesView` に `statusView` と対の `ownsKeys()` (`urlPick.active || numFilter.typing || markNext.active`)
を足し、`U` 横取りと `usageOv.dismiss()` をそのガードの内側に入れる。
status 側の「ガードは横取りだけに掛ける (委譲には掛けない)」というコメントがそのまま使える。
