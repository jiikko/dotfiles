# 冗長な防御を外すときは、それが「マスク」していた failure mode を先に列挙する — なぜ・実例

ルール本文: `~/dotfiles/_claude/rules/list-masked-failure-modes-before-removing-guard.md`（`~/.claude/rules/` に link され、毎セッション起動時に読まれる）。
この文書は起動時には読まれない。ルールの根拠・起源・実例を保存し、ルールを疑う・改訂する・却下するときに読む。

## なぜ (obaket issue 544、2026-08-26 の実測)

upload の宛先重複を `stat` で事前確認する preflight があった。SMB では wire 自身が
`FILE_CREATE` で同じことを強制するので、**preflight は重複**していた。「冗長だから外す」の
判断自体は正しかった。

見落としたのは、preflight が同時に **「adapter が `options.overwrite` を wire へ流し忘れても
同名 upload を止める」マスク**でもあったこと。外した結果:

- `SMBAdapter.uploadFileStreaming` の **引数 1 個だけ**が silent overwrite (= データ消失) を
  防ぐ状態になった
- しかもその引数は **落としてもコンパイルが通る**。呼び出し側 (`WriteOptions.overwrite`) と
  ライブラリ側 (`SMBClientSession.upload(overwrite:)`) の **両方の既定値が危険側の `true`**
- 敵対的レビューは `overwrite: options.overwrite` → `overwrite: true` の **1 トークン変異**で
  **CI が完全に green のままデータが消える**ことを実証した

**自己レビューでは最後まで出なかった観点**で、独立した 3 観点のレビューのうち **2 本が到達**した
(= 一人では見えにくいが、視点を変えれば見えるタイプの穴)。

## 発動点を「置換」へ広げた起源 (obaket 695, 2026-09-02)

eager proxy を lazy wrapper に置き換えたとき、旧実装の `onTermination { consumer.cancel() }` を引き継がず、
push 型 source の producer Task が生き残った。「外す」と自覚していなかった (置換のつもり) ので本ルールが発動せず、
変異 4 本 all red の後に敵対レビューが出した。
