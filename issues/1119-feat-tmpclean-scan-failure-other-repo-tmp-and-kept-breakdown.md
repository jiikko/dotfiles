# 1119 (feat): tmpclean が走査の失敗を「候補 0 件」と返す・他の repo の tmp/ に手が無い・残した理由ごとの容量とパスが出ない (cleanup-tmp-dir を回して出た改善点)

起票日: 2026-11-27

## 概要

2026-11-27 に、my-products の app (vlc-multi-video-player) のセッションで skill `cleanup-tmp-dir` を通しで回し、詰まった所を codex に読ませて改善点を挙げてもらった
(ユーザー指示)。skill の文面で直せるものは同じ日に直した (下の「直したもの」)。この issue は、`bin/tmpclean` (`src/tmpclean/`) に手を入れる必要があるものを残す。

その日の数: scratchpad 1,665 件 / `$TMPDIR` 15,808 件 (候補 0。`kept (新しい): 3283`) / DerivedData 43GB (候補 0) / Preferences の UUID 入りの plist 39,955 件・112 形 /
その repo の `tmp/` 99GB / worktree の登録 0 件。移せたのは Preferences だけだった。

## 詳細 (効く順。コードは 2026-11-27 の master で確かめた)

1. **走査の失敗が「候補 0 件」の成功になる**。`src/tmpclean/candidate.go` の `readDirSorted` は `os.ReadDir` の失敗を `nil` で返し、
   `collectDerivedData` (`derived_data.go`) などの呼び出し元は空の一覧として扱う。根を読めなかったときと、本当に何も無いときを区別できない。
   Preferences の列挙 (`prefs.go` の `readPrefsNames`) と sweep (`sweep.go` の `os.ReadDir`) は、読めなければ失敗を返す。誤って消す向きには広がらないが、「掃除するものは無い」と誤って報告する。
   直し方の案: 根の列挙はエラーを返し、根の走査の失敗 (止める) と、個別の項目の判定不能 (残す) を分ける。
   DerivedData と Preferences の「根が存在しないので 0 件」は今の仕様なので、そのまま残す (無いことと読めないことを分ける)
2. **dotfiles 以外の repo の `tmp/` に手が無い**。skill は容量を見せるだけで、tmpclean に `repo-tmp` の種類は無い (`selection.validate`)。
   その日はここが一番大きかった (99GB。DerivedData 全体の 2 倍超)。他の repo の `make clean-tmp` を呼ばないのは意図した制約 (同名の target が `DRY_RUN` を
   解釈するとは限らない) なので、制約を外すのではなく、tmpclean に「名指しした repo の `tmp/` の直下の、古くて開かれていない dir をゴミ箱へ移す」plan / apply を足す案。
   置き場の規約 (`~/.claude/CLAUDE.md`「一時ファイルの配置」。`tmp/<作業名>-<日付>/` と `tmp/<skill 名>/<session-id>/`) の単位で選べると、承認の表が読める。
   ただし「`tmp/` の直下の dir」だけを単位にすると `tmp/<skill 名>/<session-id>/` を session ごとに選べず、親の `<skill 名>/` は新しい session が 1 つあるだけで全部残る。
   どの深さを単位にするかを先に決める。今の検査点 (`apply.go` の `trashGuard`) は任意の repo の根を受け付けないので、種類を足すだけでは動かず、承認する根・深さ・単位を検査点に足す
3. **worktree の数え方が文章だけで、毎回 40 行ほどの bash を書く**。そのうえ `git submodule foreach --recursive` は入れ子の 1 つで失敗すると後ろを列挙しない
   (その日は `apps/SnapTrim/project-templates` で fatal。0 件という結果を、submodule を 1 つずつ回して確かめ直した)。skill には注意を足したが、
   読み取りだけのコマンド (列挙した repo と submodule の数・失敗した枝・「走査が完了して 0 件」と「未完了」の区別を出す) にする方が確か
4. **残した理由ごとの容量とパスが出ない**。plan の出力は `kept (<理由>): <件数>` だけ (内部の `kept` はパスも持つが、`cli.go` の `runPlan` が件数にまとめて捨てる)。DerivedData は 43GB で候補 0 件
   (`kept (info.plist が無く、同じ hash の持ち主が在る・分からない): 52`・`kept (持ち主が在る・分からない): 13`) で、そのうち何 GB が「分からない」で残っているのかが分からない。
   `ownerGoneOutside` は「持ち主が在る」と「分からない」を同じ `false` で返す。案: 在る・消えた・分からないを分け、理由ごとの件数・容量・大きい順のパスを追加の指定で出す
   (全部の dir の容量を測ると遅いので、常には測らない)。
   三つに分けても、同じ hash の持ち主が全部消えているときだけ候補にする条件 (`derived_data.go` の `gone` の集約) は変えない (変えると守りが外れる)
5. **Preferences の根拠調べを毎回スクリプトにしている**。112 形に `rg --fixed-strings` を掛ける分類を自分で書いた。今のテストのコードに出てくる形は 8 つだけで、
   79 形はどこにも出てこない (テストが helper に置き換わった後の残り)。読み取りだけの補助 (形ごとに、テストのコード・production のコード・issue での出方を表にする) があると速い。
   あわせて、`-prefs-nonempty` は中身ありと「空かを判定できない」をまとめて候補にする。空・中身あり・判定できないを別々に選べると、承認の説明と候補が 1 対 1 になる
6. **`session` を選んでいなくても scratchpad の根を必須にする** (`gatherWorld` が `resolveRoots` を無条件に呼ぶ。README に既知の制約として書いてある)。
   あわせて、`checkSelf` の停止 (`自分の scratchpad (<ID>) が根の下に 1 つだけ見つからない (2 個)`) は、止まる種類と見つかったパスを出さない。
   その日は、同じ session id の dir が 2 つの project の下に在った (デスクトップアプリのセッション)。重複を無条件に許すと自分の project の導出の検証が弱まるので、
   出力を足す (止まる種類・一致したパス) 方を先にする

## 直したもの (2026-11-27。skill の文面)

- 手順 4: plan は質問の直前に作る (分類に時間がかかると 1 時間の期限が切れる) / `自分の scratchpad … 1 つだけ見つからない` も `session` を外して作り直してよいと明記
- 手順 5: 中身ありの plan は空のものを含み、判定できないものも候補になると質問に書く
- 手順 7: 文の切れ目の抜け
- Preferences: helper が名前を組み立てる形を根拠ありにする条件 (生成規則の記録 + production が接頭辞を使っていない) / 分類の結果を `prefs-evidence.tsv` に残す
- worktree: `submodule foreach --recursive` が途中で止まる注意と、数え直し方

## 採らなかった指摘

- Apple container の service が止まっているときに数えない: 今のままでよい (数えるために起動すると副作用が増える。codex も同じ判断)

## 進捗

- [x] 起票の前に codex の反証レビューを通した (2026-11-27。P1 なし。採ったもの: 1 の Preferences 側を確認済みに直す・根が無いときの 0 件は残す / 2 の単位と検査点の注意 / 4 の表題と `gone` の条件)。
  採らなかったもの: 「毎回 40 行は確認できない」(その日に書いた走査のスクリプトの行数で、コードからは確かめられない事実)
- [x] codex に読ませ、指摘をコードと突き合わせた (1・4・6 は `candidate.go` / `derived_data.go` / `live.go` で確認。2・3・5 は skill の文面と当日の実行で確認)
- [ ] 1 (走査の失敗を止める) を直す。安全機構の変更なので、変異と敵対レビューを通す
- [ ] 2〜6 は、どれを作るかをユーザーが決めてから
