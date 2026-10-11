# 710 (feat): glogx の F (filetree) を出したまま C-g で閉じたら、次の C-g で最後の位置の filetree から開く

> 🚨 **担当中: dotfiles-4e**（2026-10-11〜）

起票日: 2026-10-11

## 概要

ユーザー要望 (2026-10-11): glogx の `F` で treefiler (filetree) を表示している最中に `C-g` で閉じ、もう一度 `C-g` で開いたら、
最後に表示していた位置の treefiler を表示してほしい。

今は、開き直すと git log の一覧から始まり、`F` を押し直すと起動したフォルダの初期状態 (カーソル・開いたフォルダはリセット) に戻る。

## 現状 (2026-10-11 に実コードで確認)

- `C-g` は tmux の `bind -n C-g` (`_tmux.conf`) が glogx を popup で開くキーで、glogx の中では即終了 (`tui.go` の `ctrl+g`)。
  閉じるたびにプロセスが終わるので、`filerView` (`filer_view.go`) がプロセス内に持っている状態 (同じ dir なら前の状態を使う `toggle`) は
  開き直しに効かない
- glogx には「開いたまま終了したら次の起動で復元する」記憶が 3 画面ぶんある: issues viewer (`issues_state.go`)・doctor
  (`doctor_resume.go`)・ratelimit (`ratelimit_resume.go`)。どれも `quitWith` で `remember*Screen` を呼び、TTL は `issuesStateTTL`
  (30 分)、開いていないまま終了したら消す。**F の画面 (filer) にはこれが無い**
- 終わり方は 2 経路ある。`C-g` / `C-c` は `tui.go` のキーの分岐が routing より前に `quit()` へ入るので、filer は shown のまま
  `quitWith` に届く。filer の `q` / `Esc` (= glogx ごと終える。issues viewer と同じ) は `filer_view.go` の `routeKeyToFiler` が
  `m.filerV.hide()` (`f.Close()` を呼ぶ) の後で `m.quit()` する。`quitWith` 自身は filer の `Close` を呼ばないので、treefiler の
  Remember place (`treefiler/filer/places.go` の `savePlace`。`Close` の中) は、設定が on でも `C-g` / `C-c` では保存されない
  (`q` / `Esc` では保存される)。Remember place の既定は off (`docs/treefiler-spec.md` の設定表)
- F が開く dir は repo root ではなく glogx の cwd (`fullscreen.go` の `openFullScreen` が `m.filerV.toggle(currentDir())`。
  tmux の popup は `-d '#{pane_current_path}'`)
- filer の状態 (開いたフォルダ・カーソル・起動 dir) を外から取り出す・当てる公開 API は無い (`savedPlace` / `restorePlace` は非公開で、
  どちらも先頭で `Remember` を見る)。`filer.New` の中でも `restorePlace` が走る (Remember が on のとき)

## 対応方針

- issues / doctor / ratelimit と同じ作法で `filer_resume.go` を足す: F を出したまま glogx を終えたら画面を保存し、出していなければ消す。
  `q` / `Esc` は `hide()` の前に保存する (issues viewer の `q` も記憶を残したまま終える。`issues_view.go` の q の注記)
- 照合は repo ではなく「F を開いた dir」の文字列 (起動時の `currentDir()` と比べる)。サブディレクトリで開いた F を別のサブディレクトリで
  復元しない。git の fork も要らない
- 「最後に表示していた位置」= 開いていたフォルダとカーソルのパス。treefiler に公開の型と 2 つのメソッドを足す (例: `Place` /
  `(*Model).Place()` / `(*Model).RestorePlace(Place)`)。`savePlace` / `restorePlace` は同じ中身を使い、`Remember` を見るのは保存・読み込みの
  入口だけにする (C-g のトグルの記憶は Remember place と別の機能。Remember place は TTL 無しで「次に同じ場所で起動したとき」)。
  `New` の中の復元 (Remember が on のとき) の後に当てて上書きする
- 消えたパスは飛ばす。カーソルのパスが消えていたら動かさない (`restorePlace` と同じ。`New` が置いた最初の子に残る)
- 起動時の復元の優先順位は最下位: ratelimit / doctor / issues の復元が当たったら filer は開かない (全画面ビューアは 1 枚。`fullscreen.go` の
  並びでも filer が最下位)。開くときは `openFullScreen` と同じ経路 (`watchCmd` の張り直し・usage を閉じる) を通す
- 保存は他の 3 画面と同じ作法: JSON・`writeAtomic` (`cache_write_discipline_test.go` が強制)・TTL は `issuesStateTTL` (30 分)・
  未来の時刻を弾く。保存できなくても終了は妨げない

## 受け入れ条件

- [ ] F を出したまま C-g で閉じ、30 分以内に同じ dir で C-g で開くと、F が開いた状態で、閉じる前の開いたフォルダとカーソルが戻る
- [ ] F の `q` / `Esc` で終えたときも同じく戻る
- [ ] F を閉じてから終了したら、次の起動は git log の一覧から始まる
- [ ] TTL を過ぎた・別の dir で開いた・保存先が壊れている・他の画面の復元が当たったときは、通常の起動に落ちる (起動を妨げない)
- [ ] 保存したパスが消えていても落ちない (飛ばす)
- [ ] テストは issues / doctor の復元のテストと同じ形 (保存 → 起動 → 復元) で、保存の呼び出しを外す変異で red になる

## 関連ファイル

- `src/glogx/filer_view.go` (`filerView.toggle` / `hide`) / `src/glogx/tui.go` (`quitWith` と `remember*Screen`、`ctrl+g`)
- `src/glogx/issues_state.go` / `src/glogx/doctor_resume.go` / `src/glogx/ratelimit_resume.go` (作法の正本)
- `src/treefiler/filer/places.go` (`savePlace` / `restorePlace` / `savedPlace`)
- `docs/treefiler-spec.md` (Remember place)

## 進捗

- 2026-10-11: 起票
- 2026-10-11: 反証レビュー (sonnet、read-only)。採用: `q` / `Esc` の経路は `hide()` で `Close` を呼ぶので「glogx の終了では保存されない」は
  C-g / C-c だけに当たる (P1。現状と方針に反映) / F が開くのは cwd なので照合は repo でなく dir (P1) / filer に状態を出し入れする公開 API が
  無い (P2) / 起動時の 3 画面の復元との優先順位 (P2) / 消えたカーソルは親へ落ちるのでなく動かない (P3) / writeAtomic・未来の時刻 (P3)。
  反証できなかった主張: TTL 30 分・3 画面の remember と remove・Remember place の既定 off・`savePlace` が `Close` の中・C-g の bind。
  判断: `q` / `Esc` も記憶を残す (issues viewer に揃えた)。TTL は他の 3 画面と同じ 30 分
