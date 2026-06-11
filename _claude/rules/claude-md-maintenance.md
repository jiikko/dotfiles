# CLAUDE.md の保守ルール — 触ったら直す、定期レビューしない

## ルール

- **CLAUDE.md は「触ったディレクトリで読み、乖離していたら同じ PR で直す」**。code の一部として扱い、コードと一緒に更新する
- **定期レビューはしない** (chore 化して「日付だけ更新」のような形骸化を生む)。乖離は touch base で見つけて即修正する。触っている範囲は文脈が新鮮なので乖離に気づきやすい
- **CLAUDE.md は「Why」を保存する**。「What (何があるか)」はコード自体が真の出典。制約・過去事故・前提 (= 文脈なしでは判断を誤る部分) を残す
- 乖離を見つけたら TODO / FIXME で残さず、その場で直す

## 「乖離」の検出シグナル (見つけたら同じ PR で修正)

構造的乖離 (物理的に存在しなくなったもの):

- rename 済みのファイル名 / シンボル名が旧名のまま
- 削除されたファイル / 関数が参照されている
- ディレクトリ構成表が実体と食い違う / 参照先 CLAUDE.md が存在しない
- コードサンプルの行が現コードに存在しない

意味的乖離 (物理的にはあるが意味が変わったもの):

- 「Don't do X」の X がすでに別パターンに移行済み
- 警告の根拠だった workaround が root cause 修正で不要になった
- 外部サービスの制約がすでに緩和 / 仕様変更されている
- 依存先 (「現状の foo.js に依存」) が rewrite されて前提が崩れた
- SDK / ライブラリのバージョン前提がメジャー更新で不要になった
- ぼやきポイントとして書かれた項目が解消済み

## 適用タイミング

| トリガー | やること |
|---|---|
| ディレクトリ X 配下を touch | `X/CLAUDE.md` を読み、乖離していれば直す |
| ファイル rename / 削除 | 旧名を `grep -r "<old-name>" **/CLAUDE.md` で点検して更新 |
| バグの root cause を修正 | 該当 workaround の警告が CLAUDE.md に残っていないか確認、直っていたら削除 |
| レビューで「CLAUDE.md と実装が食い違う」と指摘 | 即時修正 (rationale comment だけで閉じない) |
| 依頼と無関係だが気づいた乖離 | 軽微なら同じ PR で直す、大規模なら issue 化 |

## CLAUDE.md を作るタイミング

以下のいずれかを満たすディレクトリにだけ作る。「とりあえず置く」は禁止 (形骸化 CLAUDE.md はノイズ):

- そのディレクトリ固有の規約 / gotcha / 制約が 3 個以上ある
- 親 CLAUDE.md から頻繁に参照される
- 新しい人が読まないと必ず事故る規約がある
- ファイル 5 個以上 + 責務が複数で入口の地図が必要

## CLAUDE.md を消すタイミング

- ディレクトリ自体が削除された / 規約が親に統合できる程度に縮小した / 中身が「コードを読めば自明」だけになった (Why が消えて What だけ残った)

## 書き方

- ✓ 制約の発生源を明示し、「どうなれば変更可能か」を一行添える
- ✓ シンボル名 / ファイルパスを grep で追える形で書く。**行番号で位置を pin しない** (1 行の増減で無言にドリフトする。file 名 + symbol 名で書く)
- ✓ 親 CLAUDE.md にある規約を重複させない (継承前提。重複は片方の更新漏れを生む)
- ❌ 「絶対に変更不可」/ 「TODO」/ コードを読めば自明な目次 / issue 番号だけで中身なし / 「過去にバグった」だけ (再現条件・失敗モードを書く)

## やること / やらないこと

- ✓ コードを touch する前後に該当ディレクトリの CLAUDE.md を読み、乖離は同じ PR で直す
- ✓ 不要になった警告 (root cause 修正済みの workaround) は削除する
- ✗ 定期 CLAUDE.md レビュー会 / 1 PR で複数ディレクトリの「全部見直し」
- ✗ 「あとで直す」コメントを CLAUDE.md に残す / 形骸化した CLAUDE.md を置きっぱなしにする

## 関連

- [`claude-md-layer-prompt.md`](claude-md-layer-prompt.md) — 新規作成の問いかけルール
- [`pending-issue-rationale-in-code.md`](pending-issue-rationale-in-code.md) — 「Why を残す」同思想
- Anthropic blog: [How Claude Code works in large codebases](https://claude.com/blog/how-claude-code-works-in-large-codebases-best-practices-and-where-to-start) — "lean and layered" CLAUDE.md の出典
