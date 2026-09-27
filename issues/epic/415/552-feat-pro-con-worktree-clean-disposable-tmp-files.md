# 552 (feat): worktree clean が tmp/ の使い捨てファイルだけを理由に worktree を残し続ける

起票日: 2026-09-27

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

dogfooding (2026-09-27、550 の初回の予定で `pro-con worktree clean --yes` を本物に回した) で分かったこと。
残した 26 個のうち 14 個は「消すと戻せない無視されたファイルがある」が理由で、その中身はほぼ `tmp/` の見本・変異のスクリプト・ログ
(`tmp/mut-456.log`・`tmp/pro-con-455-sample.py`・`tmp/493/*.ans` 等)。PG は作業のたびに `tmp/` へ書くので、
予定が毎日回るようになっても、この 14 個は減らず、同じ形で増え続ける。

## 今の形

- 492 の敵対的レビュー (P0) で、`git worktree remove` が --force なしでも無視されたファイルを消すことが分かり、無視されたファイルは
  空のディレクトリと go_autobuild の産物だけを通す形にした (`wtclean/git.go` の `rebuildable`。それ以外は `wtclean/judge.go` が残す側にする)。`.env`・`settings.local.json`・入れ子の repo を守るため
- `tmp/` は `~/.gitignore_global` で ignore されていて、dotfiles の `.gitignore` には無い (CLAUDE.md「一時ファイルの配置」)

## 対応方針 (案)

- `tmp/` の下だけは「使い捨て」として扱い、消してよい側に入れる案。その場合も、消す前に `refs/pro-con/removed/` のような退避が要るか
  (見本や計測のログを後で見たくなることはある) を決める
- 退避するなら置き場 (例: 状態の置き場の下に worktree の名前ごと) と、いつ消すか (N 日) を決める
- 🚨 `tmp/` の外の無視されたファイル (.env 等) は今までどおり残す。判定を広げすぎない
- 🚨 `tmp/` を使い捨てとするのは dotfiles の決まり (CLAUDE.md「一時ファイルの配置」)。pro-con が扱うほかの repo の `tmp/` が同じ意味とは限らない
  (本物のデータを置いている repo がありうる)。repo ごとに決める口 (config.toml の repo の設定か、repo 側の印) が要るかを先に決める

## 決定 (2026-09-27、人の回答。pro-con カード C-006)

- **使い捨ての repo は config.toml の `disposable_tmp` (repo のパスの列) で決める**。書いていない repo の `tmp/` は今までどおり残す。
  repo 側の印 (追跡するファイル) は PG のブランチで足せてしまうので採らない。キーを書かなければ既定は `["~/dotfiles"]`
  (tmp/ を使い捨てと決めているのは dotfiles の CLAUDE.md だけ。`pm_repo` の既定と同じ扱い)。`disposable_tmp = []` ならどの repo も見なさない
- **退避してから消し、退避は 30 日後に消す**。CLAUDE.md の「tmp/ を消す前に issue や doc が指しているパスでないか確かめる」は機械で確かめられないので、
  30 日のあいだ退避から戻せるようにして代える
  - 置き場: `<状態の置き場>/wtclean-tmp/<repo>/<worktree の名前>/tmp/...` (本物は `~/.local/state/pro-con/live/wtclean-tmp/`。同じ名前の退避が既にあれば `<名前>.2`)
  - 日数: 退避のディレクトリを作ってから 30 日 (`wtclean.TmpKeep`。`make clean-tmp` の既定と同じ)。`pro-con worktree clean --yes` (毎日 04:00 の予定) のたびに消す
  - 出力: `worktree clean` の一覧の理由に「tmp/ の無視されたファイル N 件は wtclean-tmp/<repo>/<名前>/ へ退避してから消す」、結果の行に退避した先の絶対パスを出す。
    553 の画面では、人が決める行 (Ask) の理由に同じ件数、D (消す) の結果に退避した先が出る (tmp/ だけの worktree は自動で消せる側になり、残した一覧には出ない)
- 使い捨てとみなすのは **worktree の直下の `tmp/` の下の無視されたファイルだけ**。`src/x/tmp/` (`.gitignore` の `tmp/` はどの深さにも効く)・追跡していない tmp/ のファイル・
  tmp/ の外の `.env` 等は今までどおり残す理由になる

## 受け入れ条件

- [x] `tmp/` の下のファイルだけが理由で残っていた worktree が、予定の片付けで消える (か退避してから消える)
  — 予定が回すのと同じ `runWorktree clean --yes` で確認 (`TestWorktreeCleanYesStashesTmp`)。本物の予定での観測は取り込み・`~/dotfiles` への pull の後
- [x] `tmp/` の外の無視されたファイルがある worktree は今までどおり残る (`TestJudgeDisposableTmp` の `.env`・`src/tmp/`。設定に無い repo の tmp/ も残る)

## 進捗

- 2026-09-27 (C-006): `pro-con: worktree clean が disposable_tmp の repo の tmp/ を退避してから worktree を消す (552)`
  - 判定: `wtclean/git.go` の `readStatus` が直下の tmp/ の無視されたファイルを `ignored` から分け、`judge` が `Verdict.Tmp` に載せる。
    `Inputs.DisposableTmp` は `pro-con worktree clean`・dispatcher の Settle・設定画面 (553) の 3 経路とも同じ設定から入れる (`disposableTmpRepos`)
  - 消し方: `wtclean/stash.go` の `removeTree` が退避 → 読み直し (退避の間にできた無視されたファイルがあれば戻して止まる) → `git worktree remove`。
    消せなければ退避したファイルを元へ戻す。状態の置き場が分からなければ消さない
  - テストは新規 10 本 (wtclean 7・config 1・main 2) と既存 1 本 (TestNewDispatcherWiresRolesAndCodex) への追記。変異 14 本 (判定・退避の戻し・読み直し・30 日の境目・同名の退避・配線) を全部捕まえた
  - codex の敵対的レビュー (4 件):
    - 採った: 戻すときに、退避の間に元の場所へ同じ名前でできたファイルを上書きしていた → 上書きせず退避に残して失敗を返す (`TestStashUndoKeepsNewerFile`)
    - 反証した: 「`TestRemoveTreeRestoresTmpOnFailure` の git が断る subtest は git まで届いていない」→ 追跡していないファイルは読み直しでは止めないので届く
      (removeWorktree の失敗で戻さない変異をその subtest が捕まえた)
    - 採らない (記録): ① 最後の読み直しと `git worktree remove` の間にできた無視されたファイルは失う — 窓は 0 にできない (clean.go 冒頭の方針)。
      今までは tmp/ の外も含めて読み直しすら無かったので、今より悪くはならない ② 状態の置き場と worktree が別の volume だと rename が EXDEV で失敗する —
      消さずに残す側に倒れる (失わない)。今の配置 (~/.local/state と ~/dotfiles・~/src) は同じ volume ③ `wtclean-tmp` を人が symlink にすると、
      prune はリンク先の 30 日を過ぎたディレクトリを消す — pro-con 自身は作らない。置き場を移したいという意図の symlink ならリンク先も退避なので、防御は足さない
  - 検証: `make -C src/pro-con test` (go test -race ./...) 全パッケージ緑・`CGO_ENABLED=0 make -C src/pro-con lint` 緑
    (cgo ありだと golangci-lint のビルドが Command Line Tools の SDK 27.0 の .tbd を clang が読めずに落ちる。この変更とは別の環境の問題)
  - 残り: 本物の予定 (04:00) で、残っていた 14 個のうち tmp/ だけのものが消えるかを取り込みの後に見る

## 関連

- 492 (worktree clean) / 550 (予定) / `wtclean/git.go` (`rebuildable`) / `wtclean/judge.go`

## 並行するカードとの衝突の見積もり (2026-09-27、PM。pro-con カード C-006)

- この issue (C-006): `wtclean/judge.go` / `wtclean/git.go` の `rebuildable`。変える判断は「無視されたファイルのうち、消してよい側に何を入れるか」
- C-005 (553): 残した worktree を設定画面のディスクのタブに出し、人が消す/残すを選ぶ。消す操作は `pro-con worktree clean` と**同じ判定の 1 か所**を通す
  (553 の「決定」節)。削除したカードの worktree の扱い (`cardOf` / `store.Purged`) も変えうる
- → 同じ判定 (残す/消すの理由) を別々に変えるので **C-005 の後に積む** (`--after C-005`)。
  取り込みの係は、553 の画面に出る「残した理由」が、この issue で `tmp/` を消す側へ移した後も正しいか (`tmp/` だけの worktree が一覧に出なくなるか) をテストで見る
- C-002 (556。カードの色) / C-003 (555。止め直しの記録) とは重ならない
