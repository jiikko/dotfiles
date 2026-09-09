# human: Ruby の参照検索を sidecar 索引にするか、ripgrep のまま確定するか決める

起票日: 2026-09-09
カテゴリ: human（人間しかできない判断。データが溜まるまで決められない）
期限: 2026-10-09
（**1 か月使ってから**見る。それより早く見ても行が溜まっていない）
出典: [issue 334](done/334-ruby-references-call-site-index.md) の todolist 最後の 1 行

## 何を決めてほしいか

`<C-k>`（参照検索）の Ruby メソッドを **ripgrep のまま確定する**か、
**呼び出し側索引の sidecar 化に進む**か。

## いつ・どうやって

nvim で `:DotfilesRefsStats` を実行し、**「Ruby:」の行**を見る。

```
Ruby: ripgrep N 回 / LSP (定数など) M 回 / rg の直後に LSP へ引き直し K 回 (X%)
```

## 判定基準（数字で決める。迷わないように固定しておく）

| 条件 | 判断 |
|---|---|
| **Ruby の ripgrep が 20 回未満** | まだ決めない。期限を 1 か月延ばす（母数が足りない） |
| 引き直し **X < 10%** | **ripgrep のまま確定**。334 を done へ送り、`refs_usage` の記録機構も消す（`DotfilesRefsReset` でログを消してから） |
| 引き直し **X >= 10%** かつ LSP（定数）が 20 回以上 | **sidecar 化を検討する**。334 の「次の判断材料」節（常駐と IPC / 差分更新 / 定数の解決）から着手 |
| 引き直し X >= 10% だが LSP が少ない | メソッドの誤ヒットだけが問題。sidecar より **rg のパターンを絞る**方が安い |

## 🚨 決めたら記録機構を片付けること

`refs_usage` は**押した語をログに平文で残す**（仕事の repo の識別子が入る）。
段階 1 の判断が出たら `:DotfilesRefsReset` でログを消し、記録自体を外すか判断する
（`nvim/lua/dotfiles/refs_usage.lua` のコメントにもその旨がある）。

## 2026-09-09 時点の状態（ここから増えた分が判断材料）

```
Ruby: ripgrep 1 / LSP 0 / 引き直し 0 (0.0%)
全体: ripgrep 11 / LSP 0 / 引き直し 1
ft 未記録 11 件 (層別に使えない古い行)
```

12 行すべて実装当日（2026-09-08）の動作確認によるもので、**実運用のデータは実質ゼロ**。

## 関連

- [issue 334](done/334-ruby-references-call-site-index.md) — 実測の記録（索引の構築時間 / メモリ / クエリ時間 / 精度差）
- `nvim/lua/dotfiles/refs_usage.lua` — 記録の実体
