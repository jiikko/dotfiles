# Ruby の LSP サーバ選択を allowlist から「Gemfile + 実起動プローブ」へ移す

種別: bug / refactor
起票: 2026-09-08

## 症状

ubiregi-server (Rails / ruby 3.1.6) で `gd` / `<C-k>` が効かない。
telescope は `No LSP References found` を出す。

## 原因 (実測)

solargraph の実体は mason の 0.60.2 で、**rbenv 3.2.2** で走っている
(`_nviminit.lua` が mason の bin を PATH 先頭へ入れるため rbenv shim より勝つ)。
一方 ubiregi-server は `.ruby-version` = 3.1.6 で bundle は `vendor/bundle/ruby/3.1.0`。
そのため solargraph は存在しない `GEM_PATH=.../vendor/bundle/ruby/3.2.0` を見にいき、
bundle の gem 解決が全滅する (`~/.local/state/nvim/lsp.log`):

```
Could not find 'rubocop' (>= 0) among 71 total gem(s)
Checked in 'GEM_PATH=/Users/koji/src/ubiregi-server/vendor/bundle/ruby/3.2.0'
[WARN] Gem <ほぼ全部> from bundle not found: Gem::MissingSpecError
```

この壊れた gem マップが索引を汚染し、**自前コードの定義解決まで外す**。

### A-B (`app/models/account_label.rb` の `ApplicationRecord` / `AccountLabel`)

| 条件 | definition | references |
|---|---|---|
| 現状 (mason 0.60.2 / ruby 3.2.2 / project の Gemfile) | `account_label.rb:1` (自分自身) | 1 件 (自分自身のみ) |
| 同じバイナリで `BUNDLE_GEMFILE` を空 Gemfile に差し替え | `application_record.rb:1` ✅ | — |
| `bundle exec solargraph` (0.50.0 / ruby 3.1.6) | `application_record.rb:1` ✅ | — |
| `~/.rbenv/shims/solargraph` (0.55.1 / ruby 3.1.6) | `application_record.rb:1` ✅ | 13 件 ✅ |
| `~/.rbenv/shims/ruby-lsp` (0.26.11 / ruby 3.1.6) | `application_record.rb:1` ✅ | 13 件 ✅ |

変えた変数は 1 つ (bundle の gem マップが解決できるか) で、他は固定。
なお現状の誤答は再現率 100% ではない (6 分ポーリングで誤答のままの run と、
正答を返した run が 1 回ずつある)。索引の状態依存。

`belongs_to` はどの条件でも 0 件 (Rails の DSL は solargraph 単体では解決できない。別問題)。

## 決めたこと

サーバ選択を「project の allowlist」から「**Gemfile があり、かつその project の ruby で
`ruby-lsp` が実際に起動できるか**」の実測プローブへ移す。

- `vim.fn.executable("ruby-lsp")` はゲットに使えない (実測: rbenv shim はどれか 1 つの
  ruby に入っていれば存在するので、どの project でも常に 1 を返す)
- 未導入の ruby では今と同じ solargraph に落ちる (劣化しない)
- プローブのコストは実測 0.12〜0.13s。root ごとに 1 回だけ実行してキャッシュする
- mason の ruby-lsp は使わない (mason の ruby で走るので同じ ABI ミスマッチになる)。
  実際 `ensure_installed` に載っているのに `mason/packages/` に入っておらず、
  `cmd` が関数のサーバは実在判定されないため**通知も出ていなかった**

gem の導入は rbenv 側で人が行う (本 issue のスコープ外):

```sh
for v in $(rbenv versions --bare); do
  case "$v" in 2.*) continue ;; esac   # ruby-lsp の required_ruby_version は >= 3.0 (実測)
  RBENV_VERSION="$v" gem install ruby-lsp --no-document
done
```

## todolist

- [ ] `M.ruby_server_for` を Gemfile + 実起動プローブへ差し替え (allowlist を廃止)
- [ ] `M.server_packages` から ruby_lsp の mason 導入を外す (enable は残す)
- [ ] `tests/nvim/lsp_ruby_server_select_check.lua` を新しい軸へ書き直す
- [ ] 変異検証 (プローブ失敗時に solargraph へ落ちるか / 二重 attach しないか)
- [ ] 実 project (ubiregi-server) で attach 先が ruby_lsp になることを確認

## 残タスク (スコープ外)

- 他の Rails project (ubipay 3.1.2 / marshmallow 3.2.2 / filetree-meta-manager 3.2.2) は
  各 ruby version へ `gem install ruby-lsp` するまで solargraph のまま。
  monolink-server は ruby 2.6.6 なので ruby-lsp を入れられない (>= 3.0 要求)
- `rbenv-default-gems` の導入 (新しい ruby を入れたときの自動追従)。未導入
- ubiregi-server 直下に 7 月上旬の Claude Code worktree が 2 つ残っており、
  tailwindcss LSP がその中まで舐めている (solargraph の索引対象は 2670 件で
  `max_files: 5000` 未満なので solargraph 側は無害。実測済み)
