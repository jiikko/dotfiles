# 718 (feat): treefiler でカーソルの項目のパスをクリップボードへコピーする

起票日: 2026-10-12

## 概要

treefiler (単体の `bin/treefiler` と glogx の `F`) で、カーソルの項目のパスをクリップボードへコピーする。Claude や別の端末へパスを渡すときに使う
(ユーザーの選択 2026-10-12)。

## 仕様 (案)

- `y`: 絶対パスをコピー / `Y`: root からの相対パスをコピー (🚨 キーは推奨で、ユーザー未確認)。成功・失敗を toast で出す (`Notice`。glogx-ui-guide §9)
- glogx の `F` の中では filer が `y` / `Y` を持つ (`Binds`)。glogx の他の画面の `y` (コミットの SHA のコピー等) とは画面が違うので衝突しない
- キー一覧 (`?`)・最下行の案内・`src/glogx/README.md` のキー表・`docs/glogx-ui-guide.md` §2・spec (`docs/treefiler-spec.md` §0.1 の表) を同じ変更で直す (ui-guide §10)

## 対応方針

- クリップボードへの書き込みは今 glogx の package main にしかない (`src/glogx/external_commands.go` の `copyToClipboard`: pbcopy / xclip を正とし、
  tmux の中では `tmux load-buffer -w -` へもおまけで積む。timeout を本命とおまけで分ける)。treefiler からは import できないので、
  **`src/tuikit` に clipboard のパッケージとして切り出し、glogx と treefiler の両方から使う** (利用者が 2 つになる。
  `src/glogx/CLAUDE.md` の「構造の判断」の第二消費者の基準に当たる)
  - 🚨 切り出すとき、glogx 側のコメントにある失敗モード (xclip の X サーバ無応答・tmux の stdin fd でハングする・timeout の分け方) を落とさない
  - treefiler の外部プロセスは `subproc` 経由 (`exec_boundary_test.go` が非テストの `os/exec` を禁じる)
- パスは表示用の名前 (termsafe を通したもの) ではなく実体のパス (`node.abs`) を渡す

## 受け入れ条件

- [ ] `y` / `Y` でコピーされ、toast が出る。失敗 (pbcopy が無い等) は ✗ の toast
- [ ] glogx の既存のコピー (SHA 等) が切り出し後も同じに動く (既存のテストが緑)
- [ ] 変異で守りを確かめたテスト (クリップボードは差し替え点で fake にする)

## 関連

- issue 716 (pending。tuikit への共通化の調査。クリップボードは「2 つ目の利用者が現れたので寄せる」例)

## 進捗

- 2026-10-12: 起票
