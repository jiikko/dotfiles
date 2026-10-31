# 1016 (test): go_autobuild の embed の展開を、本物の go list と突き合わせる差分テストを足す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-31〜）

起票日: 2026-10-31

## 概要

`bin/lib/go_autobuild.zsh` の `_go_autobuild_embed_files` (Go の `path.Match` の pattern を zsh の glob へ写して展開する) を、本物の `go list -f '{{.EmbedFiles}}'` と同じ入力で比べるテストを足す。retro 1015 の提案 D。

## 詳細

- issue 1013 では、pattern の写しの誤り (`[^` と `[!`・`\X`・GLOB_SUBST の `\`・`(b)` が quote しない `!` と `-`・範囲の端・クラスの途中の `^`) が、
  敵対的レビューの 2〜5 周目で 1 つずつ見つかった。`tests/bin/test_go_autobuild.sh` の表は手で選んだ pattern と、手で go に通して取った期待値で、
  選ばなかった形の差は見えない
- 周回の中では本物の go1.26.0 との比較を毎回その場で書いていたが、テストに残していなかった

## 対応方針

- `tests/bin/test_go_autobuild_embed_oracle.sh` を足す
  - pattern ごとに 1 つの package を作り、同じ fixture (dot・`_`・空白・`( ) [ ] ^ -`・マルチバイト・入れ子のディレクトリ) を置く
  - oracle は `go list -e` の EmbedFiles。go が拒んだ pattern は比べずに数だけ出す
  - こちらの側は production と同じ経路を通す。go が受けた package を import する main に対して `_go_autobuild_record_embeds` で記録を書き、`_go_autobuild_embed_files` で展開する
- 判定は lib の方針 (Go より広めに拾う。余分は再ビルドが増えるだけ) に合わせる
  - Go が拾うものを 1 つでも拾い損ねたら落とす
  - 余分に拾ってよいのは `.` か `_` で始まる名前だけ
  - 展開が 0 件の pattern があれば落とす
- lib に書いてある受け入れた不一致 (`[\]-^]`・NFD・`**/x`・バイナリ自身) は pattern に入れない
- go が無ければ exit 77 (skip)。CI の Tests (`dotfiles-tests` の job) は setup-go で go を入れている

## 関連ファイル

- `tests/bin/test_go_autobuild_embed_oracle.sh` (新規)
- `bin/lib/go_autobuild.zsh` (`_go_autobuild_embed_files` / `_go_autobuild_go_glob` / `_go_autobuild_record_embeds`)
- `tests/bin/test_go_autobuild.sh` (手で選んだ表。残す)

## 進捗

- [x] テストを書いた: go が受けた pattern 58 件・ファイル 179 件で緑、go が拒んだ pattern 8 件 (go1.26.0、2026-10-31。敵対的レビューの後の数)
- [x] 変異 10 本で red を確認 (`bin/mutate-verify-list`)
  - 拾い損ね: クラスの中の `!` を escape しない / クラスの途中の `^` を escape する / ディレクトリの下を辿らない / クラスの中のふつうの字に `\` を付ける
  - 0 件: クラスの外を `(b)` で quote しない (`d(1)/*` がグループになる) / クラスの外の `\X` に `\` を残す
  - 余分: `?` を `*` に写す
  - クラスの中の `\X` の quote から `]` / `!` / `[` を外す (敵対的レビューの後に足した 3 本。`[` は zsh の bad pattern で展開が落ちる)
- [x] lib の `_go_autobuild_record_embeds` の上のコメントが「go list が落ちたら記録を消す」のままだった (1013 で残す形に変えた)。直した
- [x] 敵対的レビュー (opus 1 本): 判定の部分 (comm の向き・/var と /private/var・Name と dir・read -a・余分を許す正規表現) は壊せなかった。指摘 3 件はすべて採った
  - P2: クラスの中の `\]` / `\!` の quote を外しても緑だった → pattern `s/[a\]]x` と `[\!a]*.txt` を足して red を確認
  - 未確認と言われた `\[` は、`x[\[a]y` では変異の前後で意味が同じ (zsh もクラスの中の素の `[` を字に読む)。POSIX クラスに読める `[\[:alpha:]]` とファイル `a]` を足して red を確認
  - fixture の穴: `.` か `_` で始まるディレクトリの下のファイル (Go は下ごと飛ばす) → `sub/.hd/`・`sub/_ud/` を足し、余分を許す正規表現を「途中の要素が `.` か `_` で始まる」に広げた
  - P3: 継承した `_GO_AUTOBUILD_MOD_DIR` で偽の赤 → テストの中で unset
  - 測れないと分かっているもの: 当たらなかった字面を拾う退行 (`-e` の確認を外す)。go が受けた pattern は必ず実在するので oracle の形では見えない。`tests/bin/test_go_autobuild.sh` の未解決を知らせる経路のテストが受け持つ
  - 入れ子の go.mod・symlink・build tag で外れる .go の embed は fixture に無い (未確認)
- [x] root の make test: rc=0 (新しいテストは `[ok]`)
