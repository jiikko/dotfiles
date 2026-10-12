# 718 (feat): treefiler でカーソルの項目のパスをクリップボードへコピーする

起票日: 2026-10-12

## 概要

treefiler (単体の `bin/treefiler` と glogx の `F`) で、カーソルの項目のパスをクリップボードへコピーする。Claude や別の端末へパスを渡すときに使う
(ユーザーの選択 2026-10-12)。

## 仕様 (案)

- `y`: 絶対パスをコピー / `Y`: root からの相対パスをコピー (🚨 キーは推奨で、ユーザー未確認)。成功・失敗を toast で出す (`Notice`。glogx-ui-guide §9)
- glogx の `F` の中では filer が `y` / `Y` を持つ (`Binds`)。glogx の他の画面の `y` (コミットの SHA のコピー等) とは画面が違うので衝突しない
- 直す文書: `?` の一覧 (`model.go` の help の表)・`src/treefiler/README.md` のキー表・`docs/glogx-ui-guide.md` §2 (`y` の行に filer を足す)・spec
  (`docs/treefiler-spec.md` §0.1 の表と §6.1 のキー一覧) (ui-guide §10)。`src/glogx/README.md` には filer のキー表が無いので対象外

## 対応方針

- クリップボードへの書き込みは今 glogx の package main にしかない (`src/glogx/external_commands.go` の `copyToClipboard`: pbcopy / xclip を正とし、
  tmux の中では `tmux load-buffer -w -` へもおまけで積む。timeout を本命とおまけで分ける)。treefiler からは import できないので、共有の場所へ切り出す
  - 🚨 **置き場所は `src/subproc` module の中の `subproc/clipboard` パッケージ** (起票時の案「`src/tuikit` へ」は採らない)。tuikit は subproc を require して
    おらず (tuikit は `os/exec` を直に使う)、pbcopy / tmux へ stdin をパイプで渡すこの関数は subproc の WaitDelay が要る (コメントの理由)。tuikit に
    subproc を足すと、tuikit を取り込む module のうち subproc を replace していないもの (schedkeys) と、tuikit を版で固定している restartable に波及する。
    glogx と treefiler は既に `replace subproc => ../subproc` で subproc を取り込んでいるので、subproc の中なら go.mod を変えずに済む
  - 🚨 切り出すとき、glogx 側のコメントにある失敗モードを落とさない: `load-buffer -w` の実体は OSC52 で、外側の端末が解釈しないと exit 0 のまま
    クリップボードに入らない (だから OS のコマンドが本命・tmux はおまけ) / xclip は X サーバ無応答で戻らない / WaitDelay が無いと ctx の kill 後も stdin の
    copy goroutine の write を Wait が待ち続ける / 本命とおまけの timeout を分ける。timeout の値 (`localCmdTimeout` = 5 秒。glogx では tmux の prefix の
    取得にも使う) は clipboard 側に自前の定数として持つ
  - glogx の差し替え点 (`var copyToClipboard`。テスト 5 ファイルが差し替える) は残し、既定の値を `clipboard.Copy` にする (呼び出し元 3 か所:
    `tui.go` 1 か所・`issues_view.go` 2 か所は触らない)
  - xclip の分岐は glogx の今の挙動として残す (repo は macOS のみだが、切り出しで挙動を変えない)
  - treefiler の外部プロセスは `subproc` 経由 (`src/treefiler/exec_boundary_test.go` が treefiler の非テストの `os/exec` を禁じる)
  - issue 716 (pending) の表にクリップボードは載っていない。「2 つ目の利用者が現れたので寄せる」は 716 の記述ではなく、この issue の判断
- パスは表示用の名前 (termsafe を通したもの) ではなく実体のパス (`node.abs`。`path()`) を渡す。制御文字を含む名前もそのままコピーする (貼る先で使うため)
- 成功の toast: treefiler の `m.fail` は失敗 (`OK:false`) しか積まないので、成功を積む口を足す (`Notice{OK:true}`)

## 受け入れ条件

- [ ] `y` / `Y` でコピーされ、toast が出る。失敗 (pbcopy が無い等) は ✗ の toast
- [ ] glogx の既存のコピー (SHA 等) が切り出し後も同じに動く (既存のテストが緑)
- [ ] 変異で守りを確かめたテスト (クリップボードは差し替え点で fake にする)

## 関連

- issue 716 (pending。tuikit への共通化の調査。クリップボードは「2 つ目の利用者が現れたので寄せる」例)

## 進捗

- 2026-10-12: 起票
- 2026-10-12: 反証レビュー (sonnet・読み取りのみ) で訂正: 置き場所を tuikit から `subproc/clipboard` へ (tuikit は subproc を require しておらず、
  足すと schedkeys / restartable に波及する。main が go.mod で確かめた) / 守る失敗モードの全量 (OSC52 の exit 0) / glogx の差し替え点と呼び出し元 3 か所 /
  成功の toast の口が要る / 716 の表にクリップボードは無い / 直す文書の場所。反証できなかったもの: `y` `Y` は filer で空いている / `node.abs` `node.raw` は在る /
  `Notice` `TakeNotices` は在る / glogx の他の `y` とは画面が違う
