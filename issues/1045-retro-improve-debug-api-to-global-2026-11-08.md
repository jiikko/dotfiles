# 1045 (retro): skill improve-debug-api を repo ごとの 2 本から global の 1 本へまとめた振り返り

起票日: 2026-11-08

対象は 2026-11-08 の作業。
- vlc-multi-video-player の repo の skill として `debug-api-parity` を書き (14:05)、ユーザー指示で `improve-debug-api` に改名し (15:02)、検証の手順を更新した (16:56)
- obaket へ repo の skill として移植した (17:20。vlc を指す行と「片方だけ直すとずれる」の注意を含む)。vlc にも obaket を指す行を足した (17:22)
- ユーザーの「repo local にせず global か my-products に」で、global の 1 本 (方法だけ) と、各 repo の debug API の docs の「improve-debug-api の値」節 (repo の値) に分け直した (19:34〜19:38)
- obaket の retro 1408 の提案 2・3 をまとめた 1 項と、提案 4 を global の skill に入れた

## どこで踏んだか

- **2 つ目の repo へ写すときに、既存の規範を当てなかった**。
  - `~/.claude/rules/claude-md-maintenance.md` は「同じ規約を複数の場所 (別 repo・テンプレートの派生) へ配るときは、コピーを置かず単一の正本を読み込ませる経路を作る。
    コピーが避けられないなら、ずれを機械で検出する検査を同時に置く。『直接変更せず正本を変更』の注記は同期の仕組みではない」と定めている。
  - 移植のときは、単一の正本の案を一文添えただけで、依頼どおり写した。写しの両方に「片方だけ直すとずれる」の注記を足したのは、この規範が「同期の仕組みではない」と言う形そのもの。
  - 実際の手間は小さかった (2 本だった約 2 時間の間に、どちらの skill も中身は直していない。増えたのは互いを指す行の追加と、最後の移す commit)。
    改名と検証の手順の更新は 2 本になる前の出来事で、写したことによる手間ではない。
- **umbrella (my-products) に置く案を、試さずに外した**。
  - umbrella の `.claude/rules/` は submodule のセッションにも読み込まれている。だが `.claude/skills/` が submodule から見えるかは試していない。
  - 前例の `my-products-update-apps-event` を理由に global を選んだ。この skill は以前に umbrella へ移され (my-products `c97cb3b`・dotfiles `3318a1d68`)、両側で撤回されて (`2412c82`・`e7db03c71`) global に戻っている。
    撤回の理由と、umbrella の skill が submodule から見えたかは、履歴からは確かめられていない。

## 次に効きそうな改善

- 却下: 「skill を写す前に単一の正本を検討する」の新しい規範。`claude-md-maintenance.md` の上の項が同じことを言っている。今回は、その項を「2 つ目の repo へ skill を写す」場面に当てなかった。
  発動点 (別 repo へ写そうとした瞬間) は既存の項の文言 (「同じ規約を複数の場所へ配るとき」) に含まれており、文言を変えても効き目は増えない見込み。
- 却下: 「最初の 1 本を作る時点で、他の repo でも使うかを問う」。2 本目を写す時点で既存の規範を当てれば足り、1 本目の時点で問う規範を増やすと、使われない汎用化 (先回りの抽象) を招く。
- 局所 (行動しない): umbrella の `.claude/skills/` が submodule のセッションから見えるかは、次に umbrella 向けの skill を作るときに測る (見本の skill を置き、submodule で起動したセッションの一覧に出るかで見る)。
  `my-products-update-apps-event` を umbrella から戻した理由も、そのときに履歴か本人に確かめる

## 残り

なし (全項目を却下か局所で決着)。
