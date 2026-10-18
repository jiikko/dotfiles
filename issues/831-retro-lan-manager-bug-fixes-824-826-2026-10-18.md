# 831 (retro): lan-manager のバグ直し 824〜826 の振り返り

> 🚨 **担当中: Mac (glogx)**（2026-10-18〜）

起票日: 2026-10-18

## 概要

issue 824・825・826 (lan-manager の画面・初期設定の lock・Bonjour の走査のバグ 17 件) を 1 セッションで直した回の振り返り。
敵対的レビュー (opus) を 3 本 × 2〜3 周回し、P2 を計 8 件直した。

## どこで踏んだか

1. **lint が赤いまま commit が走った**。`make lint 2>&1 | tail -1 && git status && git commit …` と書き、`&&` が見たのは `tail` の rc だった。
   次の commit で直したが、赤い commit が 1 本、履歴に残った。同じ形は `verify-execution-not-just-exit-code.md` の「成否で後段を走らせる `&&` のつなぎを
   書いた瞬間にも発動する」にすでに書いてあるのに踏んだ。push と worktree の削除の組だけは hook (`deny-piped-push-then-destroy.sh`) が止めるが、commit の組は止まらない
2. **赤いテストの出力を `| tail -3` で削って読み、どのテストが落ちたかを失った**。その後は 13 回回しても再現せず、原因は未解明のまま (issue 825 に trigger を書いた)。
   これも `verify-execution-not-just-exit-code.md` の「出力を削って読まない」に書いてある形
3. **「読めなかった回は前の値を残す」と規則を変えたとき、状態 × 読み手の表を作らずに実装した**。一覧の分類は直ったが、同じ値を読む初期設定の計画が
   古い網で「この Mac を固定する」の板をもう一度出す退行を作り、レビューで再現された。その直しも確認待ちの x を断る退行を作り、2 周目で再現された。
   表を作れという規範は `survey-receiver-guards-before-passing-new-values.md` に在る (「どちらが正か」の規則を変えるとき)
4. **新しく足した分岐の一部に変異を当てていなかった**。見出しの不変条件 (後から上がった I/F を拾う) には変異を当てたが、補助の分岐
   (resolve の「IP の無い I/F」・引き直しの印) は変異が生き残り、2 周ともレビュアーに指摘された
5. **自分で書いたテストに、偽物の終わる順序に依る競りを入れた** (偽の -G がパイプを閉じてから合図を閉じ、本体の後始末より先に次の Add が届いた)。
   baseline が時々赤くなり、変異の道具が「baseline が緑でない」で止まって気づいた。直す過程で、本体にも同じ形の取りこぼし (-G の終わる間際の Add を捨てる) が見つかった

## 次に効きそうな改善

- A. 後段の `&&` を前段の rc で止める形を、push の組だけでなく commit の組にも hook で止める
  (`make …|tail … && git commit` / `go test …|tail … && git commit`)。規範は既にあるので、字句で止められる組を機械に寄せる。
  - 切り出し先: 新規 issue (`_claude/hooks/deny-piped-push-then-destroy.sh` の拡張か、別の hook)。2 と同じく「規範はあるのに踏む」なので、規範の追記はしない
- B. 規則を変える変更 (失敗の扱い・どちらが正か) では、表を作ったことを commit message か issue に残す (表を作らずに実装したことが外から見えるようにする)。
  - 切り出し先: `_claude/rules/survey-receiver-guards-before-passing-new-values.md` の該当項へ 1 行追記の案。採るかはユーザーの判断
- C. 変異は「新しく足した分岐ごとに 1 本」を当てる (見出しの不変条件の 1 本で済ませない)。
  - 切り出し先: `_claude/rules-checklists/mutation-verify-new-tests.md` に同じ項が無いか確かめてから、無ければ 1 行追記の案
- 5 はこのテストの局所の問題なので、切り出さない (その場で直した)

## 進捗

- [ ] A〜C の切り出し (ユーザーの判断待ち)
