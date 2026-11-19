# 1093 (refactor): obaket と vlc-multi-video-player の bin/mutate の本体を devtool/bin/mutate に統合する

起票日: 2026-11-19

## 概要

変異の検証のハーネス `bin/mutate` が、my-products の obaket と vlc-multi-video-player (以下 vgp) にコピーで置かれている。
vgp のものは obaket から移植した (vgp issue 506、2026-11-08)。冒頭に「直すときは obaket の側も見る」と書いてあるが、
移植の後に obaket に入った 4 commit (並列テストの `Test case '…' on` の件数を読む。obaket issue 1418。fc7901629・25430991e・6ffb8ea09・b5ac06bc3) は vgp に入っていない。
本体を `devtool/bin/mutate` の 1 本にし、各 app の `bin/mutate` は wrapper として残す (ユーザー決定 2026-11-19: 機能は変えない、wrapper は各 repo に残してよい)。

## mutate-verify と 1 本にしない理由

`bin/mutate` は今の木で変異を当てて開始時の commit へ戻す。Xcode のインクリメンタルビルドが効く代わりに、未 commit の変更があると拒否する。
`mutate-verify` は使い捨ての worktree で当てる。未 commit の変更を持ち込める代わりに、ビルドは一から作り直しになる。
Swift のアプリでは worktree からのフルビルドは遅すぎるので、両方を残す。置き場所だけを揃える (1094)。

## 2 つのコピーの違い (2026-11-19。diff の変更行 61 行。ファイルは obaket 500 行・vgp 467 行)

- 並列テストの件数・落ちたテストの読み取り (obaket だけ。fixture `bin/testdata/mutate/xcodebuild-parallel-{passed,failed}.out` と、それを見る mutate-test の約 46 行も obaket だけ)
- 環境変数の名前 `OBAKET_MUTATE_DOTFILES_ROOT` / `VGP_MUTATE_DOTFILES_ROOT`
- `bin/reap-orphan-testing-helper` を呼ぶか (obaket は必ず呼ぶ、vgp は在るときだけ呼ぶ)
- コメント (vgp の移植の注記、obaket の `run-logged-command`・lock の文言の由来)

## 今の dotfiles への依存 (変えずに保つ性質)

- `$dotfiles_root/bin/lib/runtimeout.sh` を source して `runtimeout_resolve` で **runtimeout のバイナリを起動時に 1 回だけ**解決する (その先で `bin/lib/go_tool.sh`)
- `$dotfiles_root/bin/skill-tmpdir --if-ignored mutate` で出力の置き場を決める
- mutate-test は偽の `$WORK/dotfiles/bin/lib/runtimeout.sh` と `bin/runtimeout` を組んでこの配置を試している

## 対応方針

- obaket のものを正本にして `devtool/bin/mutate` に置く。dotfiles への依存は 1092 の起動役から環境変数 (`RUNTIMEOUT` = バイナリの絶対パス、skill-tmpdir の絶対パス) で受け取る形に替える。
  mutate-test も偽の dotfiles を組む形から、偽の環境変数を渡す形に作り直す
- 各 app の `bin/mutate` は wrapper にする: 今の環境変数 (`OBAKET_` / `VGP_MUTATE_DOTFILES_ROOT`、無ければ `~/dotfiles`) で dotfiles を見つけ、`devtool-run mutate` を絶対パスで起こす。引数・終了コード・出力はそのまま
- repo に固有の部分は wrapper から明示して渡す:
  - obaket の wrapper は reap の helper のパスを渡す。devtool の mutate は、渡された helper が無ければ今どおり落ちる (「在れば呼ぶ」に弱めない)。vgp は渡さない
  - obaket の `run-logged-command` の要約行 (`complete log:`・`parallel test cases:`) と lock の文言の照合は、他の repo では一致しないだけなので本体に残す (obaket の道具の形だと注記する)
- 自己テストは devtool の `tests/` に置き、dotfiles の `make test` で回す。各 app の `make lint` の配線 (vgp `lint-mutate`、obaket `check-mutate-harness`) は残し、中身を「wrapper 経由で devtool の自己テストを起こす」に替える
  (obaket の `LINT_GATES` と `check-lint-gate-wiring` の allowlist は、レシピを変えるなら同じ commit で揃える)
- mutate-test の e2e は `cp "$MUTATE" "$REPO/bin/mutate"` で一時 repo に置く。devtool へ移したら `MUTATE`・`FIXTURES` の基準を書き換える
- **obaket の `macOS/bin/run-logged-command-test` は `"$ROOT/../bin/mutate" --summarize` を実行し、`$ROOT/../bin/testdata/mutate` の fixture を読む** (コメントだけでなく実行の依存)。
  wrapper が残るので `bin/mutate` の呼び出しはそのまま通る。fixture は devtool へ移すので、このテストは devtool の fixture を wrapper と同じ解決で読むように直す。
  `run-logged-command` の要約の形と mutate の読み取りの契約を確かめるのはこのテスト (obaket 側) のまま。要約の形を変えたら devtool の fixture も同じ変更で直す、と両方に書く
- vgp に並列テストの読み取りが入る。機能が増える方向の変化だが、vgp の baseline の件数が統合の前後で変わらないことを変異 1 本で確かめる
- global の rule `mutation-verify-new-tests.md` の「obaket の `bin/mutate`」の記述を直す
- obaket・vgp は別の repo で並行セッションが触ることがあるので、着手の前に声をかける。app の repo は app ごとに push し、umbrella の参照を bump する

## 受け入れ条件

- [ ] devtool の mutate と自己テストが dotfiles の `make test` で走る (出力にテスト名が出る)
- [ ] 全 fixture の抽出が、統合前の obaket の mutate と同じ結果になる
- [ ] vgp・obaket の両方で、wrapper から実際の変異を 1 本当てて RED と GREEN が出る。obaket の `run-logged-command-test` が通る
- [ ] 各 app の `bin/` に判定のコピーが残っていない (`grep -c 'def summarize\|MUTATE_SUMMARY'` 等。app の worktree の残骸 `apps/obaket-wt-*` は対象から外す)

## 進捗

(未着手。1092 の後)
