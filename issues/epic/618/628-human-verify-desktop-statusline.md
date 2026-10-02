# 628 (human): Claude desktop の Code タブにステータスバーが出るかを見る (625 の確認)

起票日: 2026-10-02
期限: 2026-10-09

epic [618](618-design-claude-code-mods-migration.md) の子。[625](625-feat-mods-statusline-on-desktop.md) の受け入れ条件の確認。

## なぜ人が要るか

mod `_claude/mods/desktop-statusline` は desktop の surface にだけ描く。描いた結果は desktop アプリの画面にしか出ず、このリポジトリの検査や
headless の claude からは観測できない (`claude plugin test` は desktop の要素の表で木を検査するところまでで、実際の描画は見ない)。
また、desktop のセッションで mod が読み込まれるか・`$.session.usage()` が値を返すかも、desktop で起こしたセッションでしか確かめられない。

## 手順

1. `~/dotfiles` が 625 の commit を含んでいることを確かめる (`git -C ~/dotfiles log --oneline -5` に「desktop-statusline」の commit がある)
2. Claude desktop で **新しい** Code タブのセッションを `~/dotfiles` で開く
   (古いセッションは settings の env を起動時に読んでいるので、mods を読まない。619 の settings の env を `~/dotfiles` へ pull した
   2026-10-02 14:13 頃より前に起動したものは対象外)
3. 最初のプロンプトを送る前と、1 往復した後の両方で、入力欄の上を見る

## 期待

- 入力欄の上に 3 行出る: `~/dotfiles [ブランチ …] [Opus 5.5] [ctx:…] [effort:…]` / `5h [ 1 2 3 4 5 ] …%` / `7d [ … ] …%`
- 色が付く: ディレクトリは太字、ブランチは緑の背景、枠の消費のペースは緑 / 黄 / 赤 (CLI のステータスバーと同じ意味の色)
- 同じ時刻の CLI のステータスバー (tmux のペインの claude) と、項目と数字が同じ

## 期待と違ったら

- 何も出ない → 625 を開いたままにし、本文に「desktop で何も出なかった」と書く (mod が読まれていない / desktop の surface が `desktop` でない / 帯の描画が desktop で出ない、のどれか)
- `ステータスバーを作れない: …` が出る → その文言をそのまま 625 に書く (script の失敗の理由)
- 色が出ない / 崩れる → スクリーンショットを撮って 625 に貼る (desktop の Text が色の値を受けない可能性)
- 確かめられたら、この issue を `issues/epic/618/done/` へ移し、625 も done にする

> この issue は反証レビューを通していない (人の確認の手順だけで、主張を含まないため)。手順に誤りがあれば直してよい。
