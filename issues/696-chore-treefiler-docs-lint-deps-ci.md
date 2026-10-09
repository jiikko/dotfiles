# 696 (chore): treefiler の文書の食い違い・lint 案・依存のずれ・CI の時間

起票日: 2026-10-09

## 概要

697 の監査のうち、文書・lint・依存・CI の分。

## 詳細

文書と実装の食い違い (確かめた):

1. `docs/README.md` の索引が treefiler-spec を「**未実装**、issue 662」としている (実装済み・662 は done)。`docs/treefiler-spec.md` の冒頭も「実装前の正本」のまま
2. キーの一覧 (`?`。`filer/model.go` の helpKeys) に、タイルの `d` (diff)・`o`・`Space` が無い (README のタイルの表にはある)。README に `shift+↓` `shift+↑` (タイルの J K の別名) が無い
3. `src/treefiler/CLAUDE.md` の「filer はタイマーを張らない」が、`filer/watch.go` の `time.NewTicker(watchEvery)` (ライブ更新のポーリング) と食い違う
   (呼び出し側の tick を張らない、の意味なら言い直す)。ファイルの地図に 12 ファイル (git / walk / watch / search / shell / settings / places / diff /
   open / explode / tileview / theme) が無い

lint 案 (各案の偽陽性の見込みつき):

4. パスの前方一致を `+ string(filepath.Separator)` / `os.PathSeparator` で手組みするのを ruleguard で禁止し、共通の口に寄せる (691 の 3 の再発防止。
   今の該当は walk.go の forget と model.go の findNode・explode.go の loadPath。共通の口の中を除けば偽陽性 0 の見込み)
5. os/exec の import 禁止を、AST を走査するテスト (`exec_boundary_test.go`) から depguard の deny に移す (695 の 2 の下限の問題も消える。偽陽性 0)
6. git の起動を 1 つの口に寄せ、それ以外の `subproc.CommandContext(_, "git", ...)` を禁止する (今は git.go と diff.go の 2 箇所。692 の付け方を 1 か所に
   閉じ込める。フラグの中身は lint では見られないので、口の引数を固定するテストが別に要る)
7. `time.Now` の直呼びを forbidigo で禁止する (今は model.go の注入の既定値と main.go の 2 箇所だけ。除けば偽陽性 0)。「今」を注入する設計の退行を止める
8. lint で止められないと判断したもの: 毎フレームの確保 (関数の文脈を見られない。693 の 4 の予算テストで止める)・termsafe を通さずに canvas へ書く
   (`put` の呼び出しが 46 箇所あり、汚染を追えない。入口は tree.go・preview.go・git.go の 4 箇所)

依存:

9. treefiler / tuikit は go-colorful v1.4.0・go-runewidth v0.0.24、glogx は v1.4.1・v0.0.30。runewidth は indirect のみ (直の import は depguard が禁止)
   で実害は低い。`tests/scripts/test_tuikit_consumers_aligned.sh` は x/ansi・bubbletea・ultraviolet しか見ない。対象に足すか揃える
10. govulncheck (手元の go1.26.0) で到達する stdlib の脆弱性が 2 件 (GO-2026-4602 os・GO-2026-6088 encoding/xml)。4602 は os.Root 絡みで treefiler は
    使っていない。手元の Go を上げる / CI の版を `toolchain` 行で指す

CI (run のログと API で確かめた):

11. setup-go が毎 run・毎 job で Go 1.25.0 をダウンロードして約 13 秒 (run 37864408373 の test job: `Attempting to download 1.25.0` → `Successfully cached go`)。
    job は 32〜61 秒なので割合が大きい。runner image が持つ版に合わせるか検討 (**repo 全体の go の lane に共通**)
12. treefiler だけの push で無関係の Bench (nvim / zsh / tmux。bench-tmux 5 分 53 秒) と Tests が走る (56c93c8f の run 37863501557)。bench.yml の
    paths-ignore は issues/ と docs/ だけ (**repo 全体の仕組み**)
13. (未確認) setup-go のビルドキャッシュが go.sum の鍵で最初の 1 回しか保存されない (`Cache hit occurred on the primary key, not saving cache`)。
    Test step の所要の揺れ (4 / 19 / 27 秒) がそれ由来かは未確認

その他:

14. places の読み書きに排他が無い (2 つ同時に閉じると後の書き込みが先を消す。最悪「前回の場所が戻らない」だけ。662 の「記録のみ」にもある)。
    `\r` で終わるパスは bufio.Scanner が落とす (未確認)

## 関連

- 監査の記録: 697

## 進捗

- [ ] 未着手
