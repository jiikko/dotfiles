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

## 「統合・移設で comment を書き換えるとき」を足した起源 (obaket 831 / retro 984, 2026-09-27)

4 target の copy を正本 1 つに統合したとき、codex が comment を要約して書き直し、3 ファイルで「なぜそうしないか」が消えた
(「共有 stub を『書けるが何も起きない』形に拡張しない (God 化する)」「`executorPreference` を渡すのは Core の決定論 harness だけ」
「この default は production module のコンパイル時の強制を弱めない」)。lint もテストも緑で、削除側と並べた diff の精読でしか見つからなかった。

## 本文から移した実例 (2026-09-28 の prompt-audit。本文は規範だけにするため)

- retro 376
- retro 413: 4 つ外れて > 全テストは緑のまま。見つけたのは敵対レビュー 3 周
- obaket 875 / retro 974: 設計レビュー 2 周では出ず、実装後の敵対レビューで出た
- 実測 2026-09-04 av1ify: 旧関数と一緒に消えた KSH_ARRAYS の注意書きと同じ罠を、新実装で敵対レビューに P1 として指摘された

## 本文から移した実例 (2026-10-30。常時読み込みの字数を減らすため、rule の本文の括弧書きから移した)

- obaket retro 1106: validate を build の差し替えより前へ移しかけ、不正になった設定の旧 build が cache に残る形になった

## 本文から移した実例 (2026-11-08。常時読み込みの字数を減らすため、本文の括弧書きから移した)

- esa-cli issue 014: sync の排他の鍵をパスの文字列から実パスに置き換えたら、リンクを指し直された同じ文字列の 2 本が取れた

## 「同じ種類の張り直しを 2 回やったら作り替える」を足した起源 (obaket retro 1516, 2026-11-14)

obaket issue 1514 で、HTTP の transport と Storage の client 3 つを、5 つの段階 (1a・1b・2・3・4) に分けて SwiftPM の target へ移した。
target を足すたびに、パスや名前で対象を決めている検査 (gate の走査の根・SwiftLint の `included`・層の境界の scanner・SwiftLint の fixture のパス・
rule の paths) を列挙して張り直した。レビューで見つかった漏れは、段階 1a が scanner (新しい target を走査しない)、1b が文書の test target の一覧、
3 と 4 が SwiftLint の fixture のパス (どちらも新しいサービスのパスが無い)。張り直しの形ごとに分けると次のとおり:

- 名前の規約で拾う形にしたもの (段階 2 で、SwiftLint の `included` と scanner を `Obaket<Service>API` で拾う形に変えた): 段階 3・4 で漏れの指摘なし。
  ただし変えた時点で、scanner の glob と禁止 import の正規表現が 1 文字ずれていて、レビューで見つかった (規約で拾う形は、規約に合わない名前を
  エラーを出さずに外す。本文には入れていない。入れるなら「拾った対象を期待と照合する検査を付ける」の形になる)
- 足し忘れると落ちる形のもの (走査の根の一覧。分類の無い dir があると gate 4 本が落ちる): 漏れなし
- 列挙のまま残したもの (fixture のパス・文書の一覧): 段階 3 と 4 の 2 回とも fixture が漏れた

`Package.swift`・Xcode の scheme・TSan の対象・`.dupscan.json` も段階ごとに手で足す列挙のまま残っている (足し忘れたときに落ちるかは確かめていない)。

列挙は毎回やり直す作業なので、回数に比例して漏れる。規約で拾う形にすると列挙そのものが要らなくなり、足し忘れると落ちる形にすると
漏れが検査の赤として出る。本文の「次もあるなら」は、最後の 1 回と分かっているときに作り替えを求めないための条件。
