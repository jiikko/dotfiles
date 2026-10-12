# 720 (feat): treefiler の並び替えを選べるようにする (名前・更新の新しさ・大きさ・逆順)

起票日: 2026-10-12

## 概要

今の並びは名前順だけ (Folders first・Natural sort の切り替えはある)。更新の新しい順・大きい順・逆順を選べるようにする
(ユーザーの選択 2026-10-12)。spec では「ソートは後回し」にしていたもの (`docs/treefiler-spec.md` §0.1 の表・§0.5・§5.6)。

## 仕様 (ユーザー確認済み 2026-10-12)

- 設定の板 (`,`) の ORDER に `Sort by` (name / modified / size。`choiceItem`) と `Reverse` を足す。保存する (他の項目と同じ設定ファイル)
- キー `S` で Sort by を巡回 (name → modified → size → name)。`S` は木・タイル・glogx の F の横取り (`crossTarget`) のどこでも空いている
- **フォルダは自身の値で比べる** (ユーザー確認 2026-10-12)。modified はフォルダの mtime (直下の追加・削除・改名で変わる)。size ではフォルダ自身の大きさに
  意味が無いので、フォルダどうしは名前順にする。裏の走査 (`walk.go`) の結果は使わない
  - 走査の結果を使わない理由: 走査を頼むのは配置されたフォルダ (`m.order`) だけ (`takeBackground`)。巨大なフォルダ (仮想化の対象) では画面の近くの子にしか
    値が無く、スクロールで値が届くたびに並びが動く。値も 50,000 件で打ち切った部分値になりうる・5 秒ごとに数え直す・隠しファイルの表示 (`.`) で変わる
- Folders first は Sort by と組み合わせる (フォルダを上に置いた上で、それぞれを Sort by で並べる)
- 同じ値どうしは名前の比べ方 (Natural sort の設定に従う) で決める (並びを安定させる)
- Reverse は Sort by の比べ方だけを逆にする (Folders first は逆にしない)

## 対応方針

- 並びは `tree.go` の `sortKids(kids, foldersFirst, natural)` が持つ。呼び出しは 3 か所: `node.load` (`tree.go`。パッケージ変数
  `sortFoldersFirst` / `sortNatural` を読む。Model を持たない) / `Model.resort` (`settings.go`。`m.set` を読む) / `New` の `resort(root)`。
  比べ方を 1 つの値 (並べ方の設定の構造体) にまとめ、パッケージ変数 2 つをその 1 つに置き換えてから Sort by / Reverse を足す
  (足すたびにパッケージ変数を増やさない)。`applySettings` の並べ直しの分岐 (`folders_first` / `natural_sort` のキーで判定している) に新しいキーを足す。
  足し忘れると並びが変わらないので、並びに効く設定のキーを 1 か所の表で持つ
- ファイルの mtime / size はノードが既に持っている (`newNode` が `e.Info()` から入れる)。追加の stat は要らない
- 🚨 issue 712 の仮想化との関係: 並べ直しで子の順位が変わると見せる子の覚えの世代が変わり (`kids.go` の `visOf`。順位が変わらなければ
  `sameKids` が世代を据え置く)、巨大なフォルダでは 0.6 秒全部を動かす期間に入る (`virt.go` の `noteGeo`)。modified の並びでは書き込みの続くファイルが
  ライブ更新 (1 秒ごと) のたびに先頭へ跳び、そのたびにこの期間に入る。対処を決める: 案 A = 受ける (設計どおり。巨大なフォルダで書き込みが続くときだけ重い)、
  案 B = ライブ更新による並べ直しを間引く。まず案 A で実測し、重ければ案 B (実測値を本文に書く)
- 等価性のテスト (`filer/virt_test.go`): 乱数の操作列 (op 8 の `folders_first` と同じ形) に Sort by / Reverse の切り替えを足す。🚨 fixture は全項目の mtime を
  `Chtimes` で揃えているので、そのままでは modified / size の並びが名前順と同じになり検査にならない。子ごとに mtime と大きさをばらつかせる。
  `TREEFILER_VIRT_SEEDS=300` でも回す
- 設定: 保存の往復のテストは既存に無い (`settings_test.go` は反転と並べ直しを見るだけ) ので新しく書く。spec は「`sort` を変えたら `sort_reverse` も保存」
  (§9) と書いているが、ここでは 2 つを別の項目として 1 つずつ保存する (spec をそう直す)
- 直す文書: spec の「ソートは後回し」の記述 8 か所 (§0.1 の表・§0.5・ステータスバーの `⇅` の行・§5.6 の見出しと括弧書き・§6.1 の `o` `O` の行・
  §9 の Sort by / Reverse の行・KEYS の `o O` の行)。`model.go` の help の表のコメント「ソートを除いて」・`?` の一覧・`src/treefiler/README.md` のキー表・
  glogx-ui-guide §2

## 受け入れ条件

- [x] 4 つの並び (name / modified / size / reverse) が設定の板で、Sort by が `S` で切り替わり、保存される (保存の往復のテスト + 変異)
- [x] 巨大なフォルダで並び替えた後も、仮想化の等価性のテストが通る
- [x] spec の「ソートは後回し」の記述を直す

## 進捗

- 2026-10-12: 起票
- 2026-10-12: 反証レビュー (sonnet・読み取りのみ) とユーザーの回答で訂正: フォルダは走査の結果でなく自身の値で比べる (走査は配置されたフォルダにしか
  頼まないので巨大なフォルダで並びが揺れる。main が `takeBackground` で確かめ、ユーザーに聞き直した) / キー `S` は確定 / 「並べ替えるたびに 0.6 秒の期間に入る」は
  不正確 (順位が変わらなければ `sameKids` が世代を据え置く。本当の重さは modified でライブ更新のたびに順位が変わること) / `sortKids` は 3 引数で呼び出しは 3 か所・
  Model を持たない `node.load` からも呼ぶ / 等価性のテストの fixture は mtime が揃っていて modified の検査にならない / spec の記述は 8 か所 / 保存の往復のテストは新規。
  反証できなかったもの: 今は名前順だけ / 並びは `sortKids` / 既に実装済みではない
- 2026-10-12: 実装「feat(treefiler): 並び替えを選べるようにする (Sort by: name / modified / size・Reverse・S キー) (issue 720)」
  - 並べ方を `sortOrder` (tree.go) の 1 つの値と `less` に寄せ、並べ直しを起こす設定のキーを `orderKeys` (settings.go) の 1 か所にした。`S` は toast で今の並びを出す
    (ステータスバーの `⇅` は写さない)。root を 1 段上げたとき旧 root の node へ今の mtime と大きさを写す (敵対レビューの P3)
  - 等価性のテスト: 乱数の操作列に `S` と Reverse を足し、fixture の fNNNN のファイルの mtime と大きさをずらした。🚨 フォルダと消えないファイルは 1 時間前にそろえたまま
    (フォルダの熱の色は裏の走査の結果が届く時刻で決まる。ずらしたファイルにいちばん新しい時刻を持たせると、並び替えを外した版でも 300 本の種で 4 本が色の値の差で落ちた)。
    `TREEFILER_VIRT_SEEDS=300` で通る。fixture が名前順と違う並びを作ることを `TestVirtFixtureSortOrdersDiffer` で固定
  - 変異 11 本で red (内容は commit message)。敵対レビュー 1 周 (opus) の 3 件を直し、直しは判定を足さず直接のテストと変異で確かめたので打ち切った
  - 性能 (敵対レビューの実測): 2 万件の読み直し 1 回は name 61.6ms / modified 64.8ms / size 59.6ms。並べ直しの追加は約 5% で、大半は ReadDir と stat。
    modified で書き込みの続くファイルが毎回動く見た目は仕様どおり (未計測の体感)
  - `make -C src/treefiler test` / `lint` rc=0、root の `make test` rc=0 (1 回目は `_claude/mods/tool-elapsed` の時間に関わるテストが並列の負荷で 5 秒を超えて落ち、
    単独で走らせ直すと通った。今回の変更とは無関係)
