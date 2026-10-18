# 822 (retro): コメントの経緯の lint と一括書き直し (issue 821)・817 の CI の赤・820 の振り返り

起票日: 2026-10-18

## 概要

1 セッションで、done 済みの 817 が残していた CI の赤 (Lint / Tests) を直し、820 (staticcheck のキャッシュ)・retro 815 を片付け、
821 で「コメントの経緯・出所の語」の lint を置いて 4,188 件を 24 件まで書き直した (sonnet 7 体に領域を分けて任せ、差分を機械で検閲)。

## どこで踏んだか

### 1. patch を当てた後、commit するファイルを「未ステージの差分」で拾い、269 ファイルを漏らした

- 別の worktree の差分を `git apply --3way` で当てると、変更は index に置かれる。`git diff --name-only` (未ステージ) は一覧のファイル
  1 つしか返さず、それを pathspec にして commit・push した。master は「一覧だけ下がって本文が変わっていない」状態になり Lint が赤
  (1 commit の間。すぐ残りを commit した)
- commit の後の確認は既存の規範にある (`commit-with-pathspec.md` の「commit 後に `git show --stat HEAD` を読み、想定したファイルが全部入っているか目視する」)。
  stat の 1 ファイルを見て「入っている」と読んだので、ここは規範を守らなかっただけ
- 既存の規範に無いのは commit **前**の一覧の取り方: pathspec を組むときに `git diff --name-only` (未ステージだけ) を使うと、index に書く道具
  (`git apply --3way`・merge の衝突の解決) の後では一覧が欠ける。一般形: **pathspec の一覧は `git diff HEAD --name-only` (index と作業ツリーの両方) で取り、
  件数を期待値 (当てたパッチのファイル数) と突き合わせる**
- 切り出し先: `_claude/rules/commit-with-pathspec.md` の「pathspec で生成物を漏らすと壊れたコミットになる」の節へ、上の一般形を 1 行追記 (新しい規範ではない。未実行。ユーザーの判断待ち)

### 2. 「コメントだけの変更」で生成物が古くなり、master の Lint が赤になった

- パッケージの説明 (`// Package x は …`) から番号を消すと、それを抜き出す `src/PACKAGES.md` が古くなる。コメントだけだからと pro-con の
  commit (86f33286) を `make test-lint` を回さずに push し、master の Lint の `test-go-packages-index` が赤になった (6eb0d937 で作り直した)
- 既存の規範で足りる (`verify-execution-not-just-exit-code.md`「最後の編集の後に、変更したファイルを実際に検査する gate を回す」)。
- 却下: 新しい規範は足さない

### 3. 起票時の数が「数え方の誤り」で 3 割少なかった

- 起票時に `git grep -E '^\s*(//|#)'` で数えたが、git grep の ERE は `\s` を空白として扱わず、インデントしたコメントを全部落としていた
  (この会話の中で同じ語彙を git grep で数えた 2,952 行 → lint の awk で 4,188 行。2,952 はどこにも記録していない概数)
- 既存の規範で足りる (`verify-execution-not-just-exit-code.md` の「抽出・判定を書いたら、同じ経路を通る canary を先に置く」。
  インデントしたコメント 1 行で較正していれば分かった)
- 却下: 新しい規範は足さない

### 4. 自作の lint の敵対的レビューが 4 周かかり、直した差分が毎回新しい穴を作った

- 1 周目の「done へ送る commit で赤になる」を「open の番号も数える」で直し、2 周目の「減った枠が残る」を「減ったら赤」で直すと
  rename が赤になり、件数で rename を組むと 3 周目で偶然の一致を突かれた。指紋で組んで 4 周目に収まった
- **一般形**: ラチェットの「一覧と実際の突き合わせ」を足すときは、先に「状態 × 操作」の表 (増えた / 減った / 0 になった / ファイルが消えた /
  rename / 走査の対象から外れた × 検査 / --update) を作り、全部のセルの期待値を書いてから実装する
- 既存の規範で足りる (`survey-receiver-guards-before-passing-new-values.md` の「『どちらが正か』の規則を決めるときは、先に『状態 × 読み手』の表を作る」。
  ラチェットの突き合わせも同じ形)。却下: 新しい規範は足さない

### 5. LC_ALL=C の awk で、日本語に掛けた `?` が最後の 1 バイトにしか効かない

- `(敵対的?|反証)レビュー` が「敵対レビュー」(的なし) に当たらず、素通りしていた (2 周目の敵対的レビューが git grep で数えた概数で 422 行)
- 局所的な罠なので提案にしない (`scripts/check_comment_provenance.sh` のコメントに残した)

## 次に効きそうな改善

- 1 の追記 (`commit-with-pathspec.md`)。発動点は既存の節と同じ (commit の直後) なので新規ルールにしない
- 2〜5 は却下 (上の各項)

## 残課題

- [ ] 1 を `_claude/rules/commit-with-pathspec.md` へ追記する (ユーザーの判断待ち)
- [x] 2〜5 は却下 (理由は各項)
