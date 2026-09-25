# 437 (feat): 新しい依頼が来たら pro-con が PM を起こして知らせる

起票日: 2026-09-25

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

今の本物のモードは、PM (依頼を受けてカードを分解する Claude の session) を pro-con が起動しない。誰かが PM の session を開いて
`pro-con card guide` を渡しておかないと、`n` で出した依頼は「依頼」の列から先へ進まない。PM が開いていても、新しいカードが来たことを
知らせる経路が無い。**実際に pro-con で開発を回すときの最大の穴** (2026-09-25 のユーザーとの話で確認)。

## 対応方針 (候補。設計は着手のときに決める)

- 依頼の列にカードが入ったら、dispatcher が PM の bg session を起こす (無ければ起動、居れば SendMessage か --resume で知らせる)。
  PM も pro-con が起動した session として記録し、終了で止める対象に入れる (終了の保証は 427 の「終了のとき…確実に止まる」の節)
- 受付 PM と担当 PM を分けるか (415 の論点 6) は、この issue では 1 つで始める
- 即時に起こす口は socket (package wake) がある。PM への知らせも dispatcher の Tick から出せる

## 関連

- 427 の残っていること / 415 の論点 6 (PM の数と役割)
- dogfooding (issue 440) では、PM の役を人間 (か Claude の対話 session) が CLI で代わりに行う
