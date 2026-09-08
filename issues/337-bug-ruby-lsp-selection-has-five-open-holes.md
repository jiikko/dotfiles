# Ruby の LSP サーバ選択に残る 5 つの穴

種別: bug
起票: 2026-09-08
出典: [332](done/332-ruby-lsp-selection-by-probe.md) の「未解決 (レビューが出したが、この commit では閉じていない)」節を分離したもの

332 で選択方式を allowlist から「Gemfile + 実起動プローブ」へ移した。実装と検証は閉じたが、
敵対レビューが出した 5 件は未対応のまま 332 の本文に残っていた。332 は 291 行あり、
本文を読まないと残件が見えないので独立させる。**新しい調査ではなく、332 で確認済みの事実の転記**。

## 1. プローブは「起動できる」の証明になっていない (実害あり)

`exe/ruby-lsp` の `--version` は OptionParser のブロックで即 `exit(0)` するので
(実測: ruby-lsp 0.26.11 の `exe/ruby-lsp:13`)、実際の起動経路
(BUNDLE_GEMFILE 未設定 → launcher → composed bundle の解決 → `bundle install`) を通らない。

**project の bundle が壊れていると「プローブは通るが server は起動しない」になる。**
このとき solargraph は既に抑止されているので、**Ruby の LSP が無言で消える**。
起動失敗を検出して solargraph へ戻す仕組みは無い。

- [ ] 判定の軸を「近似 (`--version` が 0 で返る)」から「実起動」へ寄せられるか設計する
- [ ] あるいは起動失敗 (`on_exit` / `LspAttach` が来ない) を検出して solargraph へフォールバックする

## 2. `.erb` (eruby) にどのサーバも attach しない project がある

lspconfig の filetypes は ruby_lsp が `{ruby, eruby}`、solargraph が `{ruby}` (実測)。
solargraph を選んだ project の `.erb` には**どのサーバも attach しない**。
allowlist 時代から同じ挙動だが、今後は「その ruby に gem を入れたか」で無言に反転する。

- [ ] 意図した挙動として受け入れるか、solargraph の filetypes に eruby を足すかを決める

## 3. `RBENV_VERSION` を export した shell から起動すると判定が全 project で同じになる (未実測)

`rbenv shell 3.1.6` した端末から nvim を開くと、プローブが常にその ruby で走るため、
ruby 2.6 の project まで ruby_lsp に倒れると考えられる。**未実測**。

- [ ] 再現するか確かめる (`RBENV_VERSION=3.1.6 nvim` で ruby 2.6 の project を開く)
- [ ] 再現するならプローブ側で `RBENV_VERSION` を落とす (project の `.ruby-version` を優先させる)

## 4. どちらのサーバがなぜ選ばれたかを見る手段が無い

選択結果が不可視で、gem を入れた後はキャッシュの無効化が無いため nvim の再起動が要る。

- [ ] 選択の理由 (Gemfile の有無 / プローブの結果 / root が repo root か) を確認できるコマンドを足す
- [ ] キャッシュを手で落とす手段を足す

## 5. mason の残留バイナリが PATH 先頭で shim に勝つ

`ensure_installed` から外しても mason はアンインストールしない。既に `mason/bin/ruby-lsp` が
入っているマシンでは、それが PATH 先頭で rbenv shim に勝つ。
**このマシンでは不在を実測済み (実害なし)**。他のマシンで踏む可能性がある。

- [ ] 起動時に `mason/bin/ruby-lsp` の存在を検出して警告するか、PATH の順序で shim を優先させる

## 関連

- [332](done/332-ruby-lsp-selection-by-probe.md) — 実装本体。「未解決」節がこの issue の出典
- [335](335-human-ruby-lsp-jump-verification.md) — 実 project での動作確認 (人間のタスク)
