# human: Ruby の LSP まわり (332 / 334) の動作確認

期限: 2026-09-15

関連: [332](done/332-ruby-lsp-selection-by-probe.md) / [334](done/334-ruby-references-call-site-index.md)

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

- [x] **`gd` / `<C-j>` が期待どおり飛ぶか** ← **機械で実測した。結論は下の「2026-09-10」節**。
      自分のコードは飛ぶ / **Rails（ActiveRecord）のメソッドは無関係な同名メソッドへ飛ぶ**。
      人に残るのは「この挙動を許容するか」の**判断**だけ
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

## 2026-09-10: `gd` の飛び先を headless で実測した — **Rails のメソッドでは外れる**

`tmux-probe-requires-socket-isolation.md` の「human に回す前に機械で測れないか一度問う」を適用。
`~/src/ubiregi-server` を実際に開き、`ruby_lsp` が attach・索引した状態で
`textDocument/definition`（`gd` = telescope の `lsp_definitions` が下で叩くもの）を投げた。

### 結果（2 例とも再現）

| 呼び出し元（実際に呼んでいるもの） | `gd` の飛び先 | 判定 |
|---|---|---|
| `account.api3_access_tokens.find_by(...)`<br>= **ActiveRecord の `find_by`** | `app/models/important_log.rb:13`<br>= `class ImportantLog; def self.find_by(logs:, checkouts:)` | ❌ **無関係な同名メソッド** |
| `role.permissions.eager_load(:resources).where(...)`<br>= **ActiveRecord の `where`** | `vendor/bundle/…/actioncable-7.0.8.7/lib/action_cable/remote_connections.rb:29`<br>= `ActionCable::RemoteConnections#where` | ❌ **別 gem の同名メソッド** |

自分のコードは飛ぶ（既に機械確認済み: `Loaders::BaseLoader` → `base_loader.rb:1` /
`wrap_error` → `base_loader.rb:35`）。

### なぜこうなるか

ruby-lsp の解決は**名前一致**で、レシーバの型を解決しない。ActiveRecord の `find_by` / `where` は
**metaprogramming で生えるので索引に入っておらず**、同名の別メソッドが拾われる。
334 の本文が「ruby-lsp のメソッド一致と同じ粒度（名前一致のみ）」と書いているのと同じ性質が、
`references` だけでなく **`definition` にも出ている**。

### 配線は正しい（同じ headless 実行で確認）

```
map gd    = 定義へジャンプ (LSP definitions)
map <C-J> = 実装へ、無ければ定義へ (impl or definition)
```

### 人に残るのは「判断」だけ

**「飛ぶか」は測れた（Rails のメソッドでは外れる）**ので、確認作業は不要。
残るのは **この挙動を許容するか**で、選択肢は:

1. **許容する**（Rails のメソッドは `gd` で追わない、と割り切る。自分のコードは正しく飛ぶ）
2. **AR のメソッドだけ別経路へ振り分ける**（`<C-k>` を ripgrep へ振り分けたのと同じ発想。
   ただし「AR のメソッドか」を静的に判定する手段が無いので、実装は重い）
3. **RBS / sig を入れて型解決させる**（ubiregi-server 側の作業。範囲が大きい）

🚨 **これは 332（solargraph → ruby_lsp）の乗り換えで**入った可能性があるが、
**solargraph 側の同条件は測っていない**（未実測）。判断の前に比較が要るなら言ってください。
