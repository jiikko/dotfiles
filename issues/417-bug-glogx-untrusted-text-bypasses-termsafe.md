# 417 (bug): glogx で外部由来の文字列が termsafe を通らずに届く経路がある (クリップボードの job URL ほか)

起票日: 2026-09-24

## 概要

git / gh / GitHub API / issue ファイル / ディレクトリ走査など、外部から来る文字列が
**termsafe (または `sanitizeDetailLine`) を通らずに**端末やクリップボードへ届く経路を洗った。
クリップボードへ生の URL を入れる P2 が 1 件、画面への経路が P3 で 6 件見つかった。

出典: 2026-09-24 の設計・品質スキャン (観点 2「外部から来る文字列の無害化」)。
P2-1 と P3-5 は起票者がコードで確認した。他はスキャン報告 (スクラッチの test で入力を流して確かめたもの)。

**深刻度を下げている事情**: bubbletea v2.0.8 の描画 (`uv.NewStyledString`。ultraviolet の `printString`) は、
SGR と OSC 8 以外のシーケンスをセルの内容に取り込み、次の文字で上書きする。OSC 52・CSI・C1 CSI は
描画されたフレームから落ちることをスキャンで確かめた。なので**画面への経路は多重防御の穴**で、
すぐに悪用できる経路ではない。この後ろ盾が無いのは、クリップボード・stdout / stderr への直接の書き出し・BiDi。

## 詳細

### P2-1 CI の job の URL が無害化されずにクリップボードへ入る (確認済み)

- 源: `src/glogx/github.go` の CheckDetail 組み立て。`Name` は `sanitizeDetailLine` を通すが、
  `URL` (`DetailsURL` / `TargetURL`。外部 CI が任意に設定できる) はそのまま
- 行き先: `src/glogx/tui.go` の `copyFocusURL` → `copyWithToast` → クリップボード。トーストの文言は
  無害化されるが、クリップボードに入る文字列は生のまま
- 同じ値を扱う `copyJobContextLines` (同ファイル) は `stripANSI(sanitizeDetailLine(...))` を通しており、
  コメントにも「job.URL は外部 CI が任意に設定でき無害化を一切通っていない」と書かれている。片方だけ守られている
- スキャンでは `https://x/\x1b]52;c;cHduZWQ=\a` がクリップボードまで残ることを確かめた。
  `ESC[201~` を仕込めば、シェルへ貼り付けたときに bracketed paste を抜けられる
- **未確認**: GitHub が `target_url` に制御文字を受け付けるか

### P3 (画面への経路。描画側で落ちるので多重防御の穴)

- **P3-1** gh の stderr が hint 行へ: `classifyGHError` の Detail (stderr) → `Warning()` → `hintPrefix`
  (`src/glogx/hint_surfaces.go`)。`main.go` は `ghErr.Warning()` を stderr へ直接出す (描画処理を通らない。読んだだけ)
- **P3-2** status viewer のヘッダのブランチ名・追跡先: `parseBranchHeader` (`src/glogx/worktree_status.go`) →
  `headerLine` (`src/glogx/status_view.go`)。ref 名には C1 や BiDi 文字が入りうる。PR の箱では無害化している
- **P3-3** git status の stderr: `v.err` を `emptyMessage` がそのまま出す (`src/glogx/status_view.go`)
- **P3-4** untracked ファイルのプレビューのエラー: `os.Open` のエラー文に生のパスが入り、`storePreview` →
  `previewPane` で出る。git diff の stderr も同じ経路
- **P3-5** termsafe が BiDi / 幅 0 の文字を落とさない (確認済み): `mustStrip` (`src/termsafe/termsafe.go`) が
  落とすのは C0 / DEL / C1 / BOM だけで、U+202E・U+2066〜2069・U+200B は素通り。描画側は U+202E を
  落とすが U+2066 (LRI) は端末まで届く (スキャン報告)。BiDi 対応の端末で、コミットの件名・ファイル名・
  issue タイトルの並びを見た目の上で入れ替えられる
- **P3-6** 作者が書ける git のフィールド (`%s` / `%an`) で SGR を許している (`LineKeepTabs`。`src/glogx/gitlog.go`)。
  git はこれらに色を付けないので、SGR があれば作者が書いたもの。`ESC[8m` で文字を隠せる。
  **意図的な設計の可能性があるので、判断を記録するだけ**

### 無害化されていると確認した経路 (再スキャンで同じ指摘を出さないため)

- gitlog.go の git のフィールド・diff と verbatim の行 (`worktree_status.go` の diff も)
- untracked ファイルの中身・status のパス (一覧・pager・discard の箱)
- CI: ログ・annotations・job 名・status 名 / PR のタイトル・head・base
- `toast.show` (toast / showWarning を通る `err.Error()` はすべてここで無害化)・`lastWarning`・
  status と issues の `setNotice`
- issue の markdown・タイトル・ファイル名・nextlink / usage の Label・Version・ratelimit 盤 / doctor
- job 文脈のコピー (`copyJobContextLines`)
- `openURLCmd` は http(s) だけを開き、shell を介さず `open` を呼ぶ
- issue の group 名とパスのコピーは、同一性のために意図的に生 (`src/glogx/issues_view.go` のコメント)
- 無害化と幅の切り詰めの順序: どこも無害化が先、または切り詰めが ESC を丸ごと保つ (`clipToWidth`) ので問題なし
- 対象外: `main.go` の git stderr のパススルー (git を直接叩くのと同じ) / クリップボードへの repo の
  Owner/Name (ローカルの remote URL 由来)

## 対応方針

1. P2-1: `copyFocusURL` で `copyJobContextLines` と同じ無害化を通す。同じ値を 2 箇所で別々に扱わないよう、
   job URL の無害化を 1 箇所 (CheckDetail の組み立て時など) に寄せる案も比べる
2. P3-5: termsafe で BiDi の制御文字 (U+202A〜202E / U+2066〜2069) を落とすか、表示用に置き換えるかを決める
3. P3-1〜4: 画面へ出す直前の関門 (`hintPrefix` / `headerLine` / `emptyMessage` / `previewPane`) で無害化する
4. P3-6: 意図的なら、その理由を `LineKeepTabs` の近くに書いて閉じる

## 関連ファイル

- `src/glogx/github.go` / `src/glogx/tui.go` (`copyFocusURL` / `copyJobContextLines`)
- `src/termsafe/termsafe.go` (`mustStrip`)
- `src/glogx/status_view.go` / `src/glogx/worktree_status.go` / `src/glogx/hint_surfaces.go`
- 既存の検査: `src/glogx/untrusted_display_test.go`

## 進捗

- [ ] P2-1 job URL のクリップボード
- [ ] P3-5 BiDi
- [ ] P3-1〜4 画面への経路
- [ ] P3-6 の判断を記録
