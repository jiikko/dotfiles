# 716 (refactor): treefiler・glogx・pro-con で同じ部品を別々に持っている (tuikit へ寄せる候補)

起票日: 2026-10-12

## 概要

dotfiles-5f の調査 (2026-10-12、読むだけ) で、2 つ以上の module が同じ処理を別々に持っている箇所が見つかった。共通の部品の置き場は
`src/tuikit` (README / CLAUDE.md)。どれも**利用者が 2 つ以上実在する**ものだけを挙げる (片方にしか無いものは寄せない。
`~/.claude/rules/verify-design-intent-before-refactor.md`)。

## 候補 (効きそうな順)

1. **入力欄の「頭 + `lineedit.Window` + キャレットの桁」の組み立て**
   - 場所: glogx `url_picker.go` `promptWindow` / treefiler `filer/model.go` `inputField` / pro-con `ui/search.go`・`ui/view.go`・`ui/answerform.go`
   - 案: `lineedit` に「頭の幅を引いて Window を呼び、キャレットの桁を頭ぶんずらす」関数を置く
   - 寄せる前に決めること: 幅の下限 1 を持つのは glogx と treefiler だけ。treefiler は 1 桁多く引いている (issue 715 の「ついでに」)
2. **treefiler `filer/tile.go` のスクロールが、tuikit にある部品と同じ中身**
   - `tileKey` の `listnav.MotionOf` の switch は `listnav.Scroll` (`tuikit/listnav/pager.go`) と同じ中身、`jumpKey` の末尾は
     `listnav.WindowOffset` (`window.go`) と同じ中身 (5f の読み。未確認)
   - 🚨 Bottom (`G`) はタイルが行を読み足してから末尾へ飛ぶ。置き換えでこの順序が変わらないことを、先にテストで固めてから置き換える
3. **スクロールバーのつまみの位置と長さ**: tuikit `layout/scrollbar.go` `Scrollbar` と treefiler `filer/tile.go` `drawTile`
   - 丸めが違う (四捨五入と切り捨て)。寄せると片方の見た目が 1 行ずれることがある (寄せるなら目で確かめる)
4. **`filer.ExecRequest` を `*exec.Cmd` に組み立てる処理が 2 つ**: treefiler `main.go` (subproc.CommandContext) と glogx
   `external_commands.go` `filerExecCommand` (exec.Command)。組み方の違いに意味があるか (WaitDelay・context の要否) を確かめてから
5. 効果の小さいもの (やるなら上と一緒に):
   - 巡回する選択の剰余算 `(i±1)%n` が 14 か所 (glogx `url_picker` / `linkJumpKey`、treefiler `jumpKey` / `panelKey` / `stepMatch` など)
   - スピナーの字形の表が 3 つ (glogx `render.go`、treefiler `model.go`、restartable `internal/ui/view.go`)。コマの選び方は違う
     (tick と 80ms の壁時計) ので寄せるのは表だけ。restartable は擬似バージョンで固定している
   - 256 色 → RGB の表 (treefiler `render.go` の xterm256 / ansi16、pro-con `switchfade.go` の color256 / basic16、`lanefade.go` の rgb256)。
     寄せ先は `tuikit/sgr`。範囲外の値を検査する場所が違う

## 見送り (理由つき。再提起するなら理由を崩す実測・事実を添える)

- 本文 pager の流用: issue 666 の D。trigger (3 つ目の pager) が引かれていない
- filer の tween (`filer/anim.go`) と `tuikit/anim`: 中身が違う (dt で進み途中から始め直す / 壁時計)
- 最下行の hint の落とし方 (treefiler `statusBar` と glogx `fitHintItems`): 方式が違う (優先度で採る / 区画ごとに固定の順。issue 279)。
  寄せると見た目が変わる
- git porcelain の解析 (treefiler `git.go` `parsePorcelain` と glogx `worktree_status.go` `parseWorktreeStatus`): 出すものとブランチの表記が違い、tuikit の責務外
- リンクのジャンプ (treefiler `tile.go` と glogx `issues/filelink.go`): 解決の規則と見せ方が違い、共通なのは剰余算だけ
- 枠 (filer `canvas.frame` と `tuikit/layout.Panel`): 描く仕組みが違う (RGB のセルの格子 / SGR 付きの文字列)
- RGB の補間 (filer `palette.go` `mix` と pro-con `mixRGB`): 3 行で型も違う

## 関連

- issue 715 (filer の入力の層。候補 1 と同じ `inputField` を触る)

## 進捗

- 2026-10-12: 起票
