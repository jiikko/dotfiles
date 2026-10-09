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
   (production の `put` の呼び出しが 44 箇所あり、汚染を追えない。無害化の入口は tree.go の 2 箇所・preview.go・git.go の計 4 箇所)

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
- [x] 1 docs/README.md の索引と spec の冒頭を「実装済み」に直した
- [x] 2 キー一覧 (`?`) のタイルの行に space・d・o を足した。README のタイルの表に Shift-↓ / Shift-↑ を足した
- [x] 3 src/treefiler/CLAUDE.md: 「タイマーを張らない」を「描画の tick を張らない (裏の goroutine と watch の ticker は filer の中)」に言い直し、
  地図に 12 ファイルと外へ出す面 (Binds・Busy・ExecDone など) を足した
- [x] 4 ruleguard `pathPrefixViaWithSep`: `x + string(filepath.Separator)` / `os.PathSeparator` (git.go の withSep 以外) と、
  `strings.HasPrefix/CutPrefix/TrimPrefix(y, x + "/")` を落とす。検出しない形 (変数・Sprintf・+=・括弧・string('/')・Contains) は gorules に書いた
- [x] 5 depguard への置き換えは採らない (695 の 2 で走査の件数を go list と突き合わせる形にし、下限の問題は消えた。同じ判定を 2 実装にしない)
- [x] 6 692 で解消済み (git の起動は subproc.GitCommand 1 本。repo 全体の gate `tests/scripts/test_git_calls_hardened.sh` が見る)
- [x] 7 forbidigo `^time\.(Now|Since|Until)$`。根の main.go は exclusions (`path: ^main\.go$`)、注入の既定値 2 か所は nolint。import の別名は検出しない
- 8 は記録のまま (lint で止められないもの)
- [x] 9 go-colorful v1.4.1 / go-runewidth v0.0.30 に揃えた (tuikit・treefiler・schedkeys・restartable)。`test_tuikit_consumers_aligned.sh` が 2 つの版も突き合わせる
- 10 未対応 (ユーザーの環境): 手元の go1.26.0 で GO-2026-6088 (1.26.6 で修正)・GO-2026-4602 (1.26.1 で修正)。CI の go1.25.0 でも同じ 2 件
  (1.25.13 / 1.25.8 で修正。govulncheck を GOTOOLCHAIN=go1.25.0 で実測)。bin/treefiler は手元の Go でビルドするので、手元の Go を上げれば消える
- 11・13 未対応 (repo 全体の CI の決定): setup-go の版の決め方は全 module に共通で、treefiler の issue では変えない
- 12 不採用: Bench の glogx のベンチは treefiler を取り込む (replace) ので、treefiler だけの push でも走るのが正しい。nvim / zsh / tmux の job も
  走るのは workflow を分けないと避けられない (repo 全体の判断)
- [x] 14 places: `\r` を含むパスも保存しない (`oneLine`。bufio.Scanner が行末の \r を落とし別のパスとして読み戻すため)。排他が無いことは記録のまま
- 変異: time.Now / time.Since を足す → lint 赤、filer/ の下の *main.go に time.Now → 赤 (除外の正規表現を固定した後)、区切りの手組みと
  HasPrefix(y, x+"/") → 赤、tuikit の go-colorful を v1.4.0 に戻す → 版の検査が赤、oneLine から \r を外す・opened の判定を戻す → テストが赤
- 敵対レビュー (opus、2 周): 1 周目の P2 3 件 (除外の `main\.go` が domain.go なども外す・time.Since / Until が素通り・HasPrefix(y, x+"/") が
  素通り) と P3 (opened の \r をテストが守っていない・✓ 行に版が出ない) を直した。2 周目は P1 / P2 なし、P3 は検出しない形の宣言と ✗ 行の文言を直した
- `make test` / `make lint` (src/treefiler) rc=0
