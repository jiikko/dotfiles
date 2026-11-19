# 1095 (task): devtool/ を別の repo (仮称 toolbox) へ切り出す

起票日: 2026-11-19

## 概要

`devtool/` を別の repo に移し、PATH と CI の参照を張り替える。

## 再開の条件

- 1092〜1094 が済み、devtool/ の境界の検査が緑のまま運用されている
- かつ、ユーザーが切り出しを指示したとき

## 切り出しのときに決めること

- 1092 の起動役 (`bin/devtool-run`) が渡している道具 (runtimeout・skill-tmpdir) を一緒に移すか、起動役ごとどこに置くか
- 各 repo の wrapper が dotfiles の checkout から起動役を探している所の張り替え
- 置き場 (clone する場所)・PATH の足し方・名前 (toolbox は Fedora の同名コマンドと重なるが、repo 名としてだけ使うなら困らない)
- CI が dotfiles の commit を固定して道具を取ってくる箇所 (例: vlc-multi-video-player の `.github/workflows/dupscan.yml` の `dotfiles_sha`) の張り替え。dupscan を devtool に移すかどうかで要否が変わる
