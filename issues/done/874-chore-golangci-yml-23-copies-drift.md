# 874 (chore): `.golangci.yml` の 23 個の写しのずれを止める (検査か生成)

起票日: 2026-10-22

## 概要

`src/*/.golangci.yml` が 23 個あり (2026-10-22 に `find src -name .golangci.yml` で数えた)、共通の塊 (`allow-parallel-runners` とその理由のコメント・
衛生系とバグ検出系の linter の一覧・govet の analyzer) を写しで持つ。linter を足す・版を上げるたびに 23 回同じ編集をしている
(872 のセッション: ba03b908 で linter の追加、0fa4cdaa で v2.5.0 → v2.15.0)。写しがずれても検査は 1 か所しか見ていない。872 の残課題から切り出した。

## 詳細

- 行数は 60〜248 行に散る (2026-10-22 時点。最小は process_supervisor、最大は glogx の 248・treefiler の 215。共通の塊に、モジュールごとの除外と
  depguard などが足されている)
- `.golangci.yml` の中身を見る既存の検査は 3 本で、どれも共通の塊の一部だけを見る:
  `tests/scripts/test_golangci_parallel_runners.sh` (全部の `allow-parallel-runners: true`)・`test_tuikit_consumers_aligned.sh` (tuikit を使う module の
  forbidigo の折り返しの禁止)・`test_tuikit_depguard_covers_parts.sh` (depguard の 2 つのリスト)。linter の一覧・govet の設定の一致は誰も見ていない
  (`test_golangci_lint_pinned.sh` は `scripts/golangci_lint.sh` のビルドと置き場所の検査で、設定の中身は見ない)
- golangci-lint v2 の設定に別ファイルを読み込む仕組みがあるかは未確認 (着手の時に v2.15.0 の文書と実物で確かめる)

## 対応方針 (2026-10-22 にユーザーと決めた)

実行時に合成する (生成物は commit しない。commit すると同じ内容が 23 個のファイルとして残り、二重管理が変わらない。ユーザー判断 2026-10-22)。

- 正本: `src/.golangci.yml` (共通の塊) と、各モジュールの `src/<module>/.golangci.local.yml` (そのモジュールの差分だけ。無いモジュールもある)
- 入口: lint は全部 `scripts/golangci_lint.sh` を通る (各 Makefile の `make lint`・CI の `_go-project.yml`・`scripts/test_changed.sh`。2026-10-22 に grep で確認。
  エディタからの起動は無い)。この script が `run` のときに合成した設定を一時ファイルへ書き、`-c` で渡す。CI も同じ入口なので、CI でも合成した設定で走る
- 合成の規則は決定的にする (ユーザー指示 2026-10-22「順序が不定にならないように」): 読む順はベース → 差分の 2 つに固定 (glob で探さない)。
  map は再帰的に合わせ、同じ鍵の値は差分が上書きする。一覧はベースの後に差分を足す。出力の鍵の並びは「ベースの順、その後に差分だけにある鍵を差分の順」。
  同じ入力からは同じバイト列が出ることをテストで固定する
- 合成は ruby (標準の YAML) で書く。python3 には YAML の標準 module が無い (手元で `import yaml` が失敗。2026-10-22)。macOS と CI の macOS runner には ruby がある
- 🚨 ベースに `run.relative-path-mode: gomod` を書く。設定の相対パス (exclusions の path) は既定で設定ファイルの置き場所から解決されるので、
  一時ファイルを `-c` で渡すと除外が効かなくなる (実測 2026-10-22・v2.15.0: treefiler の設定をモジュールの外へ写すと forbidigo が 18 件出た。
  `relative-path-mode: gomod` を足すと 0 件)
- golangci-lint 自体の `extends` / `include` は使えない (実測 2026-10-22・v2.15.0: 書いてもエラーも警告も出ずに無視され、既定の linter が有効になった)
- 素の `golangci-lint run` (script を通さない) は親のディレクトリの `src/.golangci.yml` を見つけるので、少なくとも共通の塊が効く (既定に黙って倒れない)
- CI の paths filter: 各 `src_<name>.yml` に `src/.golangci.yml` と合成の script を足し、`scripts/check_go_project_lanes.sh` で配線を固定する
- 設定の中身を読む既存の検査 3 本 (`test_golangci_parallel_runners.sh`・`test_tuikit_consumers_aligned.sh`・`test_tuikit_depguard_covers_parts.sh`) は、
  合成した結果を読む形に書き換える
- 移行の確かめ: 各モジュールで、移行前の `.golangci.yml` と合成した設定で `golangci-lint run` の結果 (指摘の件数と中身) が同じであること。
  加えて、合成した設定の意味 (YAML として読んだ値) が移行前と等しいことを機械で比べる

## 受け入れ条件

- [x] 共通の塊が 23 個でずれたら、`make test` が赤になる (写しそのものを無くした。モジュールに `.golangci.yml` を置くと lint が rc=2・`test_golangci_parallel_runners.sh` が赤)
- [x] 入口のドキュメント (`src/README.md` の `.golangci.yml` は必須、の項。今は `allow-parallel-runners` と tuikit の forbidigo だけを書いている) に、
  共通の塊の正本と直し方を書く

## 関連

- 872 (切り出し元)

## 進捗

- 2026-10-22 (dotfiles-c3): 実行時に合成する形で実装した (commit「chore(go): golangci-lint の設定を共通の塊と各モジュールの差分に分け、lint のたびに合成する」)
  - 正本は `src/.golangci.yml` と 24 本の `src/<module>/.golangci.local.yml`。差分のファイルは元の行からベースの持つ部分を落として作り、理由のコメントを残した
  - 移行の突き合わせ: 合成した設定を移行前の設定と意味 (一覧の順は無視) で比べ、23 本は同一。zundamon-kaisetsu だけ衛生系の 7 個と perfsprint の
    concat-loop が増えた (写しのずれ。説明に外す理由が無かった)。新しく出た 11 件 (intrange 3・perfsprint 8) は `--fix` と goimports で直した
  - 合成の出力は JSON (YAML の書き出しは ruby 2.6 と 3.2 でバイト列が違った)。同じ入力から同じバイト列になることを `test_golangci_config.sh` で固定
  - CI: 24 本の `src_<name>.yml` の paths に `src/.golangci.yml` と `scripts/golangci_config.rb` を足し、`check_go_project_lanes.sh` の 8. で固定
  - 変異で red を確認: 入口が -c を渡さない → `test_golangci_lint_pinned.sh` / 一覧を差分で置き換える・差分の鍵を先に並べる → `test_golangci_config.sh` /
    ベースから relative-path-mode を消す → `test_golangci_parallel_runners.sh`
  - `make test` rc=0 (24 プロジェクトの lint と test の 48 本が rc=0)・`make test-lint` rc=0
  - CI: 48f6ec9d で Go プロジェクトの 20 本・doctor・unused・Issues ほか 25 本が success。取り消された 6 本 (Tests・Lint・Bench・src/glogx・src/pro-con・src/treefiler) は
    後の bdca04fe (874 を含む) で success

## 敵対的レビューの結果 (2026-10-23・opus・読み取りのみ。push の後に通した)

普段の入口 (`make -C src/<m> lint`) では壊せなかった: 違反を入れた写しを移行前の設定と合成した設定で lint し、7 モジュールで結果が同じ。
直したもの (commit「fix(go): golangci-lint の設定の合成で、差分が共通の塊を外す形・入口の使い方の誤りを rc=2 で止める」):

- P1 差分の 1 行で共通の linter が黙って消える (`linters:` を空にする・enable を文字列にする・`linters.disable`)。合成で「差分は足すだけ」を強制した
  (書き換え・型の違う上書き・`linters.default` / `linters.disable` / `formatters.disable` は rc=2)
- P2 YAML の重複した鍵・2 つ目の文書を黙って読む → rc=2 / `.golangci.local.yaml` などの綴りの違い・モジュールの根以外からの起動で差分を読まない → rc=2 /
  `make test-changed` が共通の塊と合成の変更で Go の lint を回さない → 写像を直した
- P3 `-c` の二重指定 → rc=2。一時ファイルをやめて「中身の sha256」で決まる置き場に書き、exec に戻した (wrapper だけに TERM を送ると子が残った)。
  素の `golangci-lint run` が共通の塊だけを読むことを src/README.md に書いた
- 変異で red を確認: 書き換えを通す → test_golangci_config.sh / 重複した鍵の検査を外す → 同 / go.mod の確認を外す・綴りの確認を .golangci.yml だけにする・
  -c の確認を外す → test_golangci_lint_pinned.sh

記録だけ (直していない):
- ruby の YAML (1.1) と golangci-lint の yaml.v3 で `on` / `yes` などの解釈が違いうる。今の差分には該当する値が無い (レビューが走査で確認)。未確認リスク
- 移行で落ちたコメント: 共通の塊のバグ検出系の linter ごとの 1 行説明。doctor の「この設定が無かった間…」は経緯なので落としてよい
- 手元の glogx の lint が、消した worktree の絶対パスを持つ Go のビルドキャッシュ (cgo の生成物) で落ちる。874 より前の commit でも同じく落ち、
  空の GOCACHE では 0 件 (2026-10-23 に A/B で確認)。874 の変更とは無関係。CI は毎回キャッシュが空なので起きない
