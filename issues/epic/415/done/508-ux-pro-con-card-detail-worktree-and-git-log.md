# 508 (ux): カードの詳細に、PG の worktree のパスと git log (取り込む先より先の commit) を出す

起票日: 2026-09-26

親: [415](../415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26): 「カード詳細では worktree へのパス、git log も見れるようにしてほしい」。

## 今の形 (2026-09-26)

- 詳細 (enter) と `pro-con card show` の「進捗」の節 (469。`card/progress.go` の `ProgressLines`) は、数だけを出す:
  「commit: origin/master より N 本先 (最後の commit M 分前)」「未 commit の変更: K ファイル」「issue の本文の進捗の記録」
- PG の worktree の場所 (`<repo>/.claude/worktrees/pc-c-NNN`) とブランチの名前は、どこにも出ない。見本のコマンドや差分を見るには、人が場所を推測して打っている

## 期待する動作

- 詳細と `card show` に、PG の **worktree の絶対パス** と **ブランチの名前** を出す (コピーしてそのまま `cd` できる形。狭いときも切らずに折り返す)
- **git log**: 取り込む先 (origin/master) より先の commit を、新しい順に 1 行ずつ (短い hash・時刻・subject)。多ければ上限で切って「ほか N 本」
- 未 commit の変更は、数に加えてファイルの名前を出す (多ければ上限)
- **取り込む先との差分のコード** (2026-09-26 のユーザーの追加「コミットの一覧も欲しいのだが、base ブランチとの差分のコードが見たい」):
  `git diff <origin/master との merge-base>...HEAD` (と未 commit の分) を、詳細の中で色つきの diff として読めるようにする (ファイルごとに畳める・スクロールできる)。
  色付けは glogx の `HighlightDiff` (`src/glogx/highlight.go`。chroma) を使う。🚨 glogx の root は main パッケージで import できないので、
  markdown の整形器 (486 → `tuikit/markdown`) と同じく tuikit へ移して glogx と pro-con の両方から使う。差分が大きいときの上限 (行数・ファイル数) を決める
- worktree が無い (片付けた・まだ起動していない) カードは、そう出す
- 集め方は 469 の進捗と同じ裏の収集に載せる (画面が描くたびに git を叩かない)。`--view` でも見られる (読むだけ)
- 見た目 (節の位置・長いときの畳み方) は本体に入れる前に見本を出して人に選んでもらう

## 関連

- 469 (進捗の節) / 467 (活動) / 492 (worktree の片付け。片付けたカードは worktree が無い)

## 進捗

- 2026-09-26 (C-063): 見本 (`card attach` の sample.ans: 節の置き方 A/B/C と差分の板) からユーザーが選んだ形で実装した
  - 選んだ形: **独立した「worktree」の節を「進捗」の前に置く** (B)・commit の時刻は**相対** (9分前)・上限は **commit 5 本・ファイル 5 つ**・差分は**詳細から `D` で開く全幅の板**
  - 集め方: 469 の裏の収集 (dispatcher が 30 秒ごと) に載せた。progress.json に worktree の絶対パス・無いこと・ブランチ・commit ごとの時刻・
    未 commit の `XY パス` (`git status -z`)・差分の数を足し、差分の本文 (`git diff --merge-base <origin/master>` = commit 済みと未 commit。未追跡は入らない) は
    上限 5000 行・1 行 1000 バイトで `termsafe.PlainLine` を通して `live/diffs/<ID>.diff` に書く (同じ中身は書き直さない・集めなかったカードの本文は消す)。
    画面は `D` で開いたときにだけ裏で読んで色を付ける (描くたびに git もファイルも読まない)。`--view` でも開ける
  - 色付け: glogx の `HighlightDiff` を `tuikit/highlight` (`Diff` / `Code` / `Lang`) へ移し、glogx の diff の板・pro-con の差分の板・`tuikit/markdown` のフェンスコードが同じものを使う
    (markdown が別に持っていた chroma の Format の経路はまとめて消した)
  - 完了のカードは worktree の場所だけ出す (git は回さない。`store.Derived.Attach` が完了のカードに場所だけ足す)。まだ起動していない / 片付けた / 見つからないを言い分ける
  - `D` は glogx では doctor の板。pro-con では小文字の `d` がカードの削除なので、diff を大文字へ逃がした (`docs/glogx-ui-guide.md` の pro-con の表に書いた)
- 2026-09-26 (C-063) 敵対レビュー (サブエージェント 1 体) の 8 件のうち 7 件を直した: 切った知らせの行が板の末尾まで送っても見えない (キーの側の行数に入れた。見出しにも印)・
  作業ツリー側の rename (`git add -N` の ` R`) の元の名前を 1 件と数える・空白を含む名前の `+++` の行末の TAB が名前に混ざる・日本語の名前を引用させる (`core.quotePath=false`)・
  巨大な 1 行を丸ごと確保する (1 行 1000 バイトまでしか持たない)・板の見出しの origin/master の決め打ち・板を開いたままカードが消えると板が残る・
  progress.json を書く前に前の本文を消す (消すのは書いた後にした)
  - 残したもの (未対応のリスク): `+++` の行が無いファイル (純粋な rename・バイナリ・mode だけ) で、ディレクトリ名が ` b/` を含むと名前を取り違える。
    板は描くたびに畳みを反映した行を作り直す (5000 行で遅いかは測っていない。遅いと分かったら畳みが変わったときだけ作る)
