# 1101 (feat): tmpclean と cleanup-tmp-dir で ~/Library/Preferences の UUID 入りの plist の残骸を数え、選んだ形だけをゴミ箱へ移す

起票日: 2026-11-20

## 概要

テストが `UserDefaults(suiteName:)` などで作った、名前に UUID を含む plist が `~/Library/Preferences` に溜まる。
`tmpclean` と skill `cleanup-tmp-dir` は一時領域・DerivedData・container・worktree を扱うが、Preferences は数えない。
名前の形 (UUID を `<UUID>` に置き換えた名前) ごとに数えて見せ、ユーザーが選んだ形だけを、今の種類と同じ plan → 承認 → apply でゴミ箱へ移せるようにする。

## 詳細 (2026-11-20 に測った事実)

- `~/Library/Preferences` の直下の、名前に UUID を含み `com.apple.` で始まらない `.plist` は 11,469 件 (`com.apple.` で始まるものは 0 件)。
  多い形は `test.favorites.<UUID>` 3,186 件・`test.shortcuts.<UUID>` 1,593 件・`test.suite.<UUID>` 1,569 件・`test.settings.<UUID>` 1,287 件
  (形の集計は obaket の 1544 の作業中に `ls | sed` で取った値。tmpclean の集計で取り直して進捗に書く)
- 見た 1 件 (`test.favorites.0017A4E5-….plist`) は 42 バイトの binary plist で、中身は空の dict (`bplist00` の後の object が `0xD0` の 1 つだけ)。
  cfprefsd は suite を消した後も空の plist を書き戻す (obaket issue 457・1049 の実測)
- obaket は 1544 でテストの suite を scratch dir へ移し、新しく作らなくなった。この issue は既に溜まった分と、他の repo・手で書いたテストが作る分を片付ける

## 一時領域と違うこと (設計の前提)

- **本物の設定が隣にある**。名前に UUID を含む本物の設定もある (obaket の profile の suite は `com.jiikko.obaket.profile.<UUID>`)。
  「UUID を含めば候補」にはできないので、形ごとにユーザーが選んだものだけを候補にする
- **使用中かを lsof で確かめられない**。plist は cfprefsd が読み書きし、開いたままにしない。古さ (mtime) だけで守る
- **中身が入っているものも多い**。空のものだけに絞ると取りこぼすので、空かどうかは集計で見せる材料に留め、選ぶ条件にしない

## 対応方針

### tmpclean

- 新しい種類 `prefs`。根は `~/Library/Preferences` の直下だけ (`ByHost/` などの下の dir と、`~/Library/Containers/*/Data/Library/Preferences` は見ない。
  container の Preferences は App Store から入れたアプリと共有で、他のアプリの container を読むと macOS がアクセスの許可を求めることがある)
- 読み取りだけのサブコマンド `tmpclean prefs-shapes [-days 7] [-json]`: 直下の通常のファイルで、名前が `.plist` で終わり、UUID を 1 つ以上含み、`com.apple.` で始まらないものを形ごとに集計する。
  形ごとに件数・N 日より古い件数・空 / 中身あり / 判定できない の件数・容量・一番古い / 新しい mtime・名前の例を出す
  - 空の判定は binary plist (`bplist00`。trailer から top の object を読み、0 件の dict か) と XML (`<dict/>` だけ) を自分で読む。それ以外・壊れたものは「判定できない」 (0 件や空に畳まない)
- `tmpclean plan -kinds prefs -prefs-shape <形> [-prefs-shape <形> …] [-prefs-days 7]`: 選んだ形に当たり、N 日より古い通常のファイルだけを候補にする。
  形は `<UUID>` を含み `com.apple.` で始まらないものだけを受け付ける (それ以外は前提の検査で止める)。形は plan の選ぶ条件に入れ、ID の hash に含める
- `apply`: 今の種類と同じく候補を作り直して plan との交わりだけを、1 件ずつ古さを取り直してからゴミ箱の `tmpclean-<日時>-<ID>/prefs/<名前>` へ移す。
  1 件ずつの lsof は取らない (plist は開いたままにされず、1 万件では 1 件 0.3 秒の lsof が 1 時間になる)
- 検査点 (`trashGuard`) に Preferences の根の直下 (深さ 1) を足す。根は `strictRoots` のとき `~/Library/Preferences` に固定する

### skill cleanup-tmp-dir

- 表に `prefs` の行を足す。手順 2 で `tmpclean prefs-shapes -json` を取り、手順 3 の対象の選択肢に足す
- 形を選ぶ段: 形ごとの UUID の前の接頭辞を、`~/src` と `~/dotfiles` のテストのコード (`*Tests*`・`*_test.go`・`test/`・`tests/` の下) で探し、
  テストに出て production のコードに出ない形を「根拠あり」、それ以外を「根拠なし」に分けて表で見せる。選択肢は「根拠ありの形を全部移す」「移さない」で、一部だけなら Other に形を書く。
  根拠なしの形は候補にしない (選びたいなら Other で名指しする)
- 移した後は、移した plist の domain を cfprefsd がまだ覚えていることがあると報告に添える (読み書きする process が無ければ、再起動の後に消える)

## 受け入れ条件

- [ ] `tmpclean prefs-shapes` が形ごとの集計を出し、名前に UUID の無いもの・`com.apple.` で始まるもの・dir・symlink を数えない
- [ ] `plan -kinds prefs` が選んだ形の古いものだけを候補にし、形を選ばない・`<UUID>` を含まない形・`com.apple.` の形は rc=2 で止める
- [ ] `apply` が plan の後に更新された plist (新しくなったもの)・差し替わったもの・形の外のものを移さない
- [ ] 検査点が Preferences の根の直下以外 (下の dir・根そのもの) を拒否する
- [ ] テストを変異で red にして確かめる (形の照合を外す / 古さの取り直しを外す / 検査点の深さを変える)
- [ ] skill に種類と手順を足し、`bin/skill-eval cleanup-tmp-dir` を回す
- [ ] ホストで `prefs-shapes` を取り直し、集計の値を進捗に書く

## 検出しないもの (書いておく制約)

- 形をユーザーが選ぶので、本物の設定の形を選べば移す (ゴミ箱から戻せる。skill は根拠なしの形を既定で選ばない)
- 古さの閾値の内側で、まだ使われている suite を見分ける手段は無い (lsof に出ない)
- 移した後に、同じ domain へ書く process があれば cfprefsd が plist を作り直す
- container の中の Preferences・`ByHost/` の下

## 関連ファイル

- `src/tmpclean/` (`candidate.go`・`plan.go`・`apply.go`・`deps.go`・`cli.go`・`README.md`)
- `_claude/skills/cleanup-tmp-dir/SKILL.md`・`_claude/skill-evals/cleanup-tmp-dir/`

## 進捗

- [ ] 実装
