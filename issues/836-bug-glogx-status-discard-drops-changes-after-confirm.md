# 836 (bug): status viewer の X が、確認した後に足された変更まで捨てる (P2)

> 🚨 **担当中: air5 (glogx)**（2026-10-19〜）

起票日: 2026-10-19

## 概要

`src/glogx/internal/statusview/status_view.go` の `StatusView.runDiscard` は、実行の直前に status を取り直して
`(section, path)` で行を引き、`x` / `y` (porcelain の XY) が確認時と同じかだけを照合する。
` M` のまま中身だけ変わった編集は検出できないので、確認の後に (Claude Code や $EDITOR が) 足した変更も `git restore` で一緒に消える。
`docs/status-viewer-spec.md` 4 節の明文の条件は `(パス, XY)` の一致で、実装はその条件どおり (仕様違反ではない)。
ただし同じ節が理由に挙げる「確認中にファイルが変わっているなら、ユーザーが見て判断したものはもう存在しない」を、その条件では保証できない。
直すのは**仕様の保証の強化**になる。

## 詳細

- 発火条件: ` M` の foo を開いて右のプレビュー (diff A) を見る → 別プロセスが foo に B を足す (XY は ` M` のまま) → `X` → `y`。A と B が両方消え、B は一度も画面に出ていない。復元の手段は無い
- 右のプレビューは、一覧全体の行 (section / XY / path) が変わらない限り作り直さない (`StatusView.Receive` の `sameRows` は一覧全体を比べる。他の行が変われば消える)。同じ一覧のまま中身だけ変わる場合は、判断に使った diff が確認の数秒より長く古くなりうる
- untracked のディレクトリ行 (`clean -fd dir/`) も同じで、確認の後にそのディレクトリへ作られたファイルも消える
- 裏取り: コードと spec を読んだ (`runDiscard` の照合は `now.x != row.x || now.y != row.y` のみ)。実際の再現はしていない

## 対応方針

守るものが 2 つあり、別々に設計する (codex の反証レビューの指摘: 確認に入るときに指紋を採るだけでは、確認の前に B が入った本文のシナリオを防げない。その時点の B を含む状態が基準になり、`y` で一致して A も B も消える)。

- (a) 確認中の変更: 確認に入る時点で作業ツリー側の指紋を控え、`y` の直前に取り直して一致しなければ中止する。指紋は内容で取る (`git hash-object`。size + mtime は内容の一致を保証しない)。ディレクトリ行は中身の一覧
- (b) 画面に出していない変更: 表示した diff と基準の状態を結び付ける (プレビューを作ったときの指紋を持ち、確認に入るときに今の指紋と比べて違えばプレビューを作り直して確認をやり直す)
- spec 4 節の保証を、(a)(b) を含む形に書き直す
- 回帰テスト: (a) 確認の後に中身だけ変える / (b) プレビューの後・確認の前に中身だけ変える、のどちらも `y` で捨てず、ファイルが残る

## 関連ファイル

- `src/glogx/internal/statusview/status_view.go` (`runDiscard` / `Receive`)
- `docs/status-viewer-spec.md` 4 節

## 進捗

- [x] 指紋の照合 + spec + 回帰テスト (2026-10-19。commit「glogx: 全体監査 (844) の issue 834〜843 を直す」)
  - 指紋 (`rowPrint`): index 側の `git ls-files -s` の行 + 作業ツリー側の stat (mode・inode・大きさ・mtime・ctime)。未追跡のディレクトリ行は `git ls-files -o --exclude-standard --full-name` (= `git clean -fd` が消す集合) の各ファイル
  - 🚨 対応方針の「内容で取る (hash)」から変えた: X / y のキー処理の中で同期に採るので、hash だと大きなファイルで UI が止まる (opus のレビューの実測: 4 GiB で 1.7〜2.0 秒)。APFS で同じ大きさで書き直して 5000 回中 5000 回 stat が変わった (mtime の差は最小 21µs)。キー処理の中の所要: ファイル行 `ls-files -s` 20ms、2 万件のディレクトリの `ls-files -o` 32ms、20 万件 125ms (上限で断る)
  - (a) 確認中の変化: `y` の直前に (パス, XY, 指紋) を取り直し、違えば中止
  - (b) 見せていない変化: 最後に**描いた** diff の指紋 (`seenPrints`。プレビュー欄・全画面 diff の描画で控える) と今の指紋が違えば確認に入らず読み直す。受け取っただけ・取得に失敗した・本文が 1 行も入らない高さの diff (プレビュー欄の見出しだけ・高さ 1 の全画面 diff) は「見せた」に数えない。基準は (セクション, パス) で持つ (XY が変わっても引ける)。diff の取得の前後で指紋が違えば「確かめられなかった」
  - X を断る: repo root が分からない・未追跡のディレクトリの中が 2 万件を超える / 1 秒を超える・tracked のパスが作業ツリーではディレクトリ (submodule・ファイルをディレクトリに置き換えた形。restore が中を丸ごと消す)
  - spec 4 節の不変条件 1 を書き直した (閉じられない窓も書いた)
  - 回帰テスト: `TestRealDiscardAbortsWhenOnlyIndexChangedDuringConfirm` (MM で index だけ差し替え) / `TestRealDiscardUntrackedDirPrintFollowsCleanSet` / `TestStatusDiscardIgnoresReceivedButUnshownDiff` / `TestStatusDiscardComparesShownPrintAcrossXYChange` ほか
- レビューの中で見つけて直した、前から在った不具合: `r` で自動更新と読み込みの札が立ったまま残り自動更新が止まる (`TestStatusReloadRearmsLoadAndPoll`)・取得中の `r` でプレビューが空のまま残る (旧い世代の結果を捨てるときに取り直しを予約する。取得中の札は世代を持たないので `busyGen` で控え、閉じて開き直した後の新しい世代の札は降ろさない)・staged の rename の後に編集した行 (RM) の X が pathspec の不一致で止まる (`TestRealDiscardRenamedThenEditedRow`)・git が返すパスを TrimSpace で削り、末尾が空白の root を壊す (`ResolveRepoRoot` ほか 3 か所)
- 変異: 計 30 本超 (index の行・`--full-name`・`--exclude-standard`・上限で切り詰め・stat を大きさだけに・描画で控える・XY 込みのキーなど) がすべて red
- レビュー: opus の敵対的レビュー 1 本・codex の実装レビュー 4 周 (最後の 1 周は codex の週の枠が切れたので opus のサブエージェントで代えた)
- 残る窓 (閉じない。spec に記録): 最後の照合から restore / clean までの間の書き込み・diff の取得の間に index を A → B → A と戻す形 (`.git/index` の stat を指紋に入れると status の取得のたびに X が断られる)。共有の mmap の書き込みで stat が遅れて付く形は macOS で反例を作れていない (未確認)
- 記録だけ (安全側か被害が無い): 未追跡のディレクトリに増えた空のディレクトリ・入れ子の repo の中の変化で断る (断り過ぎ)
