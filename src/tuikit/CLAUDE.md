# tuikit

glogx の issues viewer から切り出した端末 UI 部品集 (一覧 → 詳細の遷移・演出・幅計算)。**描画フレームワークに依存しない** (bubbletea v1/v2 どちらでも使える。`caret` だけ例外)。使い方・遷移パターン・不変条件の正本は README.md。

## パッケージの地図

README.md の「パッケージ」表が正本 (各パッケージの中身と使いどころ)。要約:

- `termwidth` — 表示幅の単一情報源。行を幅で切る・揃えるときは必ずここを通す
- `widthenv` — 幅モデルが支持しない環境変数の検出
- `sgr` — 基本の ANSI 色・装飾
- `anim` — 開閉演出 (`Transition`)・進捗 (`Elapsed`)・数行スクロールの glide 演出
- `layout` — 画面合成 (`ComposeDrawer` / `Panel` / `Overlay` / `Scrollbar` 等)
- `confirm` — y/N 確認ダイアログ
- `lineedit` — 1 行入力欄 (readline 風編集キー)
- `caret` — 入力欄キャレットの端末カーソル (**tuikit で唯一 bubbletea を import する**)
- `editor` — 実ファイルを $VISUAL/$EDITOR/nvim で開くコマンド
- `toast` — 右下に数秒出る通知スタック
- `markdown` — markdown 本文を幅で整形するレンダラ
- `highlight` — diff / コードのシンタックスハイライト
- `listnav` — 一覧・本文のカーソル移動・スクロール窓の計算

## 消費者

- glogx (issues viewer 本体)・pro-con・schedkeys・ratelimit が replace で取り込む (`grep -l "replace tuikit" src/*/go.mod`)
- schedkeys は `caret` / `termwidth` を使い、`toast` だけは別実装

## ビルド・テスト

- `make -C src/tuikit lint` / `test` / `demo` (vhs で gif 再撮影)
- `examples/listdetail`, `examples/toast` — `go run ./examples/...` で実速度のデモを触れる

## 詳しくは

- README.md 全体 (遷移のパターン各種のコード例・使う側の約束・不変条件を守る仕組みの表・デモの撮り方)
