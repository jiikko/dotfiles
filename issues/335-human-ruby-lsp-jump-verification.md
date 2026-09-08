# human: Ruby の LSP まわり (332 / 334) の動作確認

期限: 2026-09-15

関連: [332](332-ruby-lsp-selection-by-probe.md) / [334](334-ruby-references-call-site-index.md)

**前提: nvim を一度終了して開き直すこと。** `vim.lsp.enable` は起動時に走るので、
開きっぱなしのインスタンスは古い設定のまま動く（今日それで 2 往復した）。
確認したいプロジェクトを開いている nvim だけでよく、他は放置してよい。

## 機械で確認済み（やり直さなくてよい）

- ubiregi-server で `ruby_lsp` (0.26.11) が attach し、solargraph は attach しない
- `Loaders::BaseLoader` → `base_loader.rb:1` / `wrap_error` → `base_loader.rb:35` /
  参照 3 件（本番の `~/dotfiles` 経由で headless 実測）
- `<C-k>` の振り分け・進捗表示・使用実績記録のロジックと配線（テスト + 変異検証）
- `<leader>K` は既存キーと衝突しない（`<leader>k` はチートシート）

## 人にしか確認できないもの

- [ ] **`gd` / `<C-j>` が期待どおり飛ぶか**（自分のコード・Rails のメソッド両方で）
- [ ] **`<C-k>` が体感で速いか**（Ruby のメソッドの上。実測 0.1 秒だが、telescope の
      描画まで含めた体感は測っていない）
- [ ] **`<C-k>` の結果が実用に足るか**。ripgrep なのでコメント・文字列・シンボルを拾う。
      足りないときは `<leader>K` で LSP に引き直す（この操作が段階 1 の観測点になる）
- [ ] **ステータスラインの表示**（lualine の右寄り）。見え方は人が見るまで分からない:
  - [ ] 起動直後に `Ruby LSP: indexing files: NN% completed` が出て、終わると消える
  - [ ] `<C-k>` / `gd` を押した瞬間に `LSP: 参照を検索中…` などが出て、返ると消える
  - [ ] 文字数が長くて他の要素（encoding / filetype / 位置）を押し出していないか
  - [ ] `documentHighlight` などで点滅していないか（出ない設計だが、目で見て確認したい）
- [ ] **1 週間ほど使ってから `:DotfilesRefsStats`**。「rg の直後に LSP へ引き直し」の率が
      高ければ sidecar 化（334 の次段階）に進む価値がある。低ければ rg のままで確定

## 気になったら教えてほしいこと

- 索引が数分かかる（起動のたびに 10 秒前後のはず。初回だけ composed bundle の構築で数分）
- `<C-k>` がまた 10 秒級に戻る（振り分けが効いていない可能性）
- ステータスラインがちらつく・幅を圧迫する
