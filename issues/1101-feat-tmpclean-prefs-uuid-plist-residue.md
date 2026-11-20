# 1101 (feat): tmpclean と cleanup-tmp-dir で ~/Library/Preferences の UUID 入りの plist の残骸を数え、選んだ形だけをゴミ箱へ移す

起票日: 2026-11-20

## 概要

テストが `UserDefaults(suiteName:)` などで作った、名前に UUID を含む plist が `~/Library/Preferences` に溜まる。
`tmpclean plan` の種類は `session`・`tmpdir`・`derived-data` で、skill `cleanup-tmp-dir` はそれに Apple container と worktree を足して扱うが、どちらも Preferences は数えない。
名前の形 (UUID を `<UUID>` に置き換えた名前) ごとに数えて見せ、ユーザーが選んだ形だけを、今の種類と同じ plan → 承認 → apply でゴミ箱へ移せるようにする。

## 詳細 (2026-11-20 に測った事実)

- `~/Library/Preferences` の直下の、名前に UUID を含み `com.apple.` で始まらない `.plist` は 11,469 件 (`com.apple.` で始まるものは 0 件)。
  多い形は `test.favorites.<UUID>` 3,186 件・`test.shortcuts.<UUID>` 1,593 件・`test.suite.<UUID>` 1,569 件・`test.settings.<UUID>` 1,287 件
  (形の集計は obaket の 1544 の作業中に `ls | sed` で取った値。tmpclean の集計で取り直して進捗に書く)
- 見た 1 件 (`test.favorites.0017A4E5-….plist`) は 42 バイトの binary plist で、中身は空の dict (`bplist00` の後の object が `0xD0` の 1 つだけ)。
  cfprefsd は suite を消した後も空の plist を書き戻す (obaket issue 457・1049 の実測)
- obaket は 1544 で、テストが名前を直書きする suite を scratch dir へ移した (production の命名規則を確かめる 3 か所と、production の factory を通る profile の suite (obaket 1554) は残る。
  1554 の残りは sandbox の container に出て、ホストの Preferences には出ない)。この issue は既に溜まった分と、他の repo のテストが作る分を片付ける

## 一時領域と違うこと (設計の前提)

- **本物の設定が隣にある**。名前に UUID を含む本物の設定もある (obaket の profile の suite は `com.jiikko.obaket.profile.<UUID>`)。
  「UUID を含めば候補」にはできないので、形ごとにユーザーが選んだものだけを候補にする
- **使用中かを lsof で確かめられない**。plist は cfprefsd が読み書きし、開いたままにしない。古さ (mtime) だけで守る
- **中身が入っているものも多い**。空のものだけに絞ると取りこぼすので、空かどうかは集計で見せる材料に留め、選ぶ条件にしない

## 対応方針

### tmpclean

- 新しい種類 `prefs`。根は `~/Library/Preferences` の直下だけ (`ByHost/` などの下の dir と、`~/Library/Containers/*/Data/Library/Preferences` は見ない。
  container は sandbox のアプリの保存先で、他のアプリの container を読むと macOS がアクセスの許可を求めることがある。`ByHost/` にはホストの識別子を名前に含む本物の設定がある)。
  根を読めない (権限など) ときは 0 件に丸めず止める
- Preferences を読むだけのサブコマンド `tmpclean prefs-shapes [-days 7] [-json]`: 直下の通常のファイルで、名前が `.plist` で終わり、UUID を 1 つ以上含み、`com.apple.` で始まらないものを形ごとに集計する。
  形は `.plist` を除いた名前の UUID を `<UUID>` に置き換えたもので、集計に出た形をそのまま plan に渡す
  形ごとに件数・N 日より古い件数・空 / 中身あり / 判定できない の件数・容量・一番古い / 新しい mtime・名前の例を出す
  - 空の判定は binary plist (`bplist00`。trailer・offset table・top の object の位置の範囲を確かめてから、top が 0 件の dict か) と XML (`<plist>` の下の dict が子の要素を持たない。`<dict/>` も `<dict></dict>` も) を自分で読む。
    それ以外・壊れたもの・1 MiB を超えるものは「判定できない」 (0 件や空に畳まない)
- `tmpclean plan -kinds prefs -prefs-shape <形> [-prefs-shape <形> …] [-prefs-days 7] [-prefs-nonempty] -out <dir>`: 選んだ形に当たり、N 日より古い通常のファイルのうち、
  空と判定できたものだけを候補にする。中身があるもの・判定できないものは `-prefs-nonempty` を付けたときだけ候補にする (値を持つ設定を移すのは、空のものと別に承認を取る)。
  形は `<UUID>` を含み `com.apple.` で始まらないものだけを受け付ける (それ以外は前提の検査で止める)。日数は 1〜3650 (大きい日数で `time.Duration` が溢れて閾値が未来になるのを止める)。
  形・日数・`-prefs-nonempty` は plan の選ぶ条件に入れ、ID の hash に含め、apply で検査し直す。条件を変えたら plan を作り直して承認を取り直す (ユーザーに見せた ID だけを `-approve` に渡す)
- `apply`: 今の種類と同じく候補を作り直して plan との交わりだけを、ゴミ箱の `tmpclean-<日時>-<ID>/prefs/<名前>` へ移す。
  plan の項目に選んだ時点の mtime を持たせ、作り直した候補と移す直前の `Lstat` の両方で、通常のファイル・mtime が plan と同じ・古いことを確かめる
  (dev/ino だけでは、同じ inode のまま書かれたものを見分けられない)。最後の確かめから rename までの窓 (1 件ぶん) は残る。
  1 件ずつの lsof は取らない (plist は開いたままにされず、1 万件では 1 件 0.3 秒の lsof が 1 時間になる)。全体の lsof の写しに出たものは残す (今の種類と同じ。出ないことは使われていない証拠にならない)
- 検査点 (`trashGuard`) は、`prefs` を選んだときだけ Preferences の根を受け付け、移す元が根の直下 (深さ 1) の通常のファイルで、名前の形が選んだ形のどれかであることを確かめる
  (候補の選定が壊れても、本物の設定・dir を通さない)。根は `strictRoots` のとき `os.UserHomeDir()` の `Library/Preferences` に固定し、経路に symlink があれば止める (DerivedData と同じ)

### skill cleanup-tmp-dir

- 表に `prefs` の行を足す。手順 2 で `tmpclean prefs-shapes -json` を取り、手順 3 の対象の選択肢に足す (選択肢が 4 つを超えるので、worktree と同じく 2 問目に分ける)
- 形を選ぶ段: 接頭辞の文字列が出てくるだけでは根拠にしない (VLC のテストは `test.settings.ABC` を掃除の対象から断る assert に書いている)。
  テストのコードの中に、その形の完全な名前を UUID と連結して suite・domain を作る箇所 (`"test.favorites.\(UUID().uuidString)"` の形) が在り、
  同じ接頭辞を production のコードが組み立てていない形だけを「根拠あり」にする。今のコードに無くても、repo の issue・git の履歴に、その形を作っていたテストの helper の記録があれば
  根拠として出典つきで見せる。それ以外は「根拠なし」で、候補にしない (選びたいなら Other で名指しする)
- 承認は 2 段に分ける: 根拠ありの形の空のもの (形ごとの件数の表)、続けて同じ形の中身あり・判定できないもの (`-prefs-nonempty` の plan を別に作り、件数を見せて別に承認を取る)
- 手順 4〜7 を `prefs` だけ選んだ場合・他と混ぜた場合の両方で通す: plan を作る条件・所要 (prefs は 1 件ずつの lsof が無い)・前後の比較 (`tmpclean count` は Preferences を数えないので `prefs-shapes` を取り直す)
- 移した後の報告に添える: 移した suite を開いている process が居れば、メモリに残った値を書き戻して plist が元の場所に作り直されることがある (UserDefaults は先にメモリを書き、ディスクへは後で書く)。
  戻すときは `moved-<ID>.jsonl` で元の場所を見て、同じ名前が作り直されていたら上書きせずに比べる (ゴミ箱の「戻す」は使えない。tmpclean は Finder の戻し先の情報を付けない)

## 受け入れ条件

- [ ] `tmpclean prefs-shapes` が形ごとの集計を出し、名前に UUID の無いもの・`com.apple.` で始まるもの・dir・symlink を数えない
- [ ] `plan -kinds prefs` が選んだ形の古い空のものだけを候補にし (`-prefs-nonempty` で中身ありも)、形を選ばない・`<UUID>` を含まない形・`com.apple.` の形・範囲外の日数は rc=2 で止める
- [ ] `apply` が plan の後に更新された plist (apply が候補を作り直した後に書かれたものを含む)・差し替わったもの・形の外のものを移さない
- [ ] 検査点が Preferences の根の直下以外 (下の dir・根そのもの)・選んでいない形・通常のファイルでないものを拒否する
- [ ] テストを変異で red にして確かめる (形の照合を外す / 古さの取り直しを外す / 検査点の深さを変える)
- [ ] skill に種類と手順を足し、`bin/skill-eval cleanup-tmp-dir` を回す
- [ ] ホストで `prefs-shapes` を取り直し、集計の値を進捗に書く

## 検出しないもの (書いておく制約)

- 形をユーザーが選ぶので、本物の設定の形を選べば移す (ゴミ箱から戻せる。skill は根拠なしの形を既定で選ばない)
- 古さの閾値の内側で、まだ使われている suite を見分ける手段は無い (lsof に出ない)
- 移した後に、同じ domain を開いていた process (保留中の書き込みを含む) が書けば、cfprefsd が plist を作り直す
- container の中の Preferences・`ByHost/` の下
- `strictRoots` の根は `os.UserHomeDir()` (= `$HOME`) から決める。`$HOME` を差し替えて起動すれば、その下の `Library/Preferences` を扱う (DerivedData と同じ)
- `prefs` だけを選んでも、今の種類と同じく scratchpad の根の実在と全体の lsof を前提にする (Claude Code を使わない機械では止まる。今回は変えない)

## 関連ファイル

- `src/tmpclean/` (`candidate.go`・`plan.go`・`apply.go`・`deps.go`・`cli.go`・`README.md`)
- `_claude/skills/cleanup-tmp-dir/SKILL.md`・`_claude/skill-evals/cleanup-tmp-dir/`

## 反証レビュー (2026-11-20、codex gpt-6.2-sol)

指摘 9 件 (P1 2 / P2 5 / P3 3)。全部を本文に入れた。却下 0 件。採り方を変えたもの:

- P1 (中身ありまで一括で承認): 空だけを選ぶ条件にしない、という元の判断は変えず、中身あり・判定できないものを `-prefs-nonempty` の別の plan と別の承認に分けた
- P2 (prefs だけでも scratchpad と全体の lsof に頼る): 今の種類と同じ前提として残し、「検出しないもの」に書いた (変えると session・tmpdir の守りの前提に触る)
- P2 (`os.UserHomeDir()` が `$HOME` に従う): DerivedData と同じ扱いとして残し、「検出しないもの」に書いた

## 進捗

- [ ] 実装
