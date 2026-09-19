# 403 (test): next/ に claim の目印があるのに、issue 本文 1 行目に担当者バナーが無い状態を検出する

起票日: 2026-09-19
工数: S (tests/issues/ に検査 1 本 + 変異確認)

## 概要

`claim-issue-in-next-and-push.md` は「claim したら issue 本文の 1 行目にも担当者と状態を書く」を要求しているが、
書き忘れを検出する仕掛けが無い。`next/` への push は hook (`next-claim-push.sh` / `next-claim-unshared.sh`) が
促すのでそこまでは到達し、**push した時点で claim が完了したように感じてバナーが落ちる** (retro 361。
2026-09-11 に 358 で発生し、照会 1 往復と推測による誤帰属を生んだ)。glogx の `n` も symlink を作るので hook では捕まらない。

## 対応方針

- `tests/issues/test_next_links_valid.sh` の隣に、`issues/next/` と `issues/epic/*/next/` の各 symlink について
  リンク先 issue の冒頭にバナー (書式は claim ルール側で確定させる) があるかを見る検査を足す
- 🚨 検査が名指しする退行 (バナーを消す変異) で red になるまでを 1 セットにする。対象 0 件を合格に畳まない

## 進捗

- [x] バナーの書式を claim ルールで確定 (`> 🚨 **担当中: <誰>**（YYYY-MM-DD〜）`。検査は既存 2 書式 `**担当中` / `**着手中` を受ける)
- [x] 検査を追加し、変異で red を確認

### 「test(issues,403): next/ の claim に担当者バナーが無ければ CI で落とす」

- `tests/issues/test_next_claims_have_banner.sh` (本体。canary 3 件で awk 判定を先に確認) と
  `…_fixtures.sh` (repo は claim 0 件のことが多く本体だけでは空の緑になるため、両側を fixture で固定)
- 判定: 最初の `## ` 見出しより前に `**担当中` / `**着手中`。対象は `next/` と `epic/*/next/` の .md (symlink と旧運用の実ファイル)
- 変異 5 本すべて red: 常にバナーあり / epic を見ない / 見出しで止めない / 失敗を数えない / 実ファイルを飛ばす
- `make test-dir DIR=tests/issues` の集約経路から 2 本とも実行を確認 (出力行あり)。shellcheck 通過
- claim ルール (書式・同じ commit で push・手順例)、`_claude/issue-rules.md`、`issues/README.md` を更新

### 「fix(issues,403): 敵対レビュー 1 周目の指摘を反映」

- P1 (採用): `NEXT/` `Epic/` `.MD` の有効な claim を 0 件扱いで見逃していた → 置き場と拡張子を大文字小文字を無視して判定、段数を `/` 分割で数える
- P2 (採用): フェンス内・HTML コメント・H1 行・散文中の `**担当中` を通していた → 行頭アンカー (`> ` `🚨 ` を許す) + フェンス除外
- P2 (採用): fixture が meta / dangling / 大文字小文字を守っていなかった → fixture 追加
- P3 (採用): 改行入りファイル名が割れる → `find -print0` + `read -d ''`
- 変異 10 本すべて red (前回 5 本 + フェンス除外 / 行頭アンカー / 大文字小文字 / meta / dangling)。実行後の一時ディレクトリ残骸ゼロ
- 受容: canary ブロックの削除は fixture からは検出できない (canary は検査自身の自己確認で、外から壊せる seam が無い)。**確実な検出手段はない**。canary の awk と本走査は同じ関数を通るので、awk の破損そのものは fixture が落とす

### 「fix(issues,403): 敵対レビュー 2 周目の指摘を反映」

- 2 周目 (修正差分が攻撃対象。/bin/bash 3.2 + /usr/bin/awk で実行): **P1 なし**
- P2 (採用): 引数の末尾が `//` だと 0 件の緑 → 末尾 `/` を全部落とす (links_valid と同じ形)
- P3 (採用): `~~~` フェンス・字下げした (3 桁までの) フェンスの中を拾っていた → fence 判定を広げた
- 変異 3 本 (末尾 / を 1 つだけ落とす / `~~~` を外す / 字下げを外す) すべて red
- 周回の打ち切り: 2 周目の修正は正規表現 1 つと既存 (links_valid) と同形のループで、fixture で直接確認した
  (adversarial §7 の例外 (a)(b))。3 周目は回さない

## 残タスク

- スコープ外: 逆向き (バナーがあるのに claim が無い = 解除時の消し忘れ) は検出しない。done へ移せば対象外になるが、claim だけ外した場合は古いバナーが残る
- スコープ外: glogx の `n` はバナーを書かないので、`n` で claim すると CI が落ちる (意図どおり。`n` にバナー挿入を足すかは別判断)
- 記録のみ (2 周目 P3): epic 名に改行を含む claim は見逃す / 閉じていないフェンスの後ろのバナーを見落として赤 /
  `>🚨` (空白なし) のような書式揺れは赤 (repo の過去のバナーは `> 🚨 **担当中` の 2 件だけで誤検出なし) /
  相対パスの引数は `cd "$ROOT_DIR"` 後に解決される (本 PR 以前からの仕様。手で変異検証するときの罠)
