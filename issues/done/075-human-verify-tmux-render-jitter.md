# 075 human: scratch popup 表示中に背景の文字がブレる件の切り分け

起票日: 2026-08-21
期限: 2026-08-28

ユーザー報告 (2026-08-21): tmux の scratch popup (prefix+t / C-t t) を開いていると、背景の
tmux の文字が「たまに微妙にズレる・ブレる」。

**現状: ブレは止まっているが、原因は確定していない**。同じ時間帯に 2 つの変更が入ったため、
どちらが効いたか切り分けが必要 (下記「人にやってほしいこと」)。

## 人にやってほしいこと

まず現在値を見る:

```sh
tmux show -g status-interval            # 0 なら「毎秒の再描画を止めた」状態
tmux show -s terminal-features | wc -l  # 5 なら terminal-features 修正が反映済み
```

そのうえで `status-interval` を 1 に戻し、scratch popup を開いてブレが再現するかを見る:

```sh
tmux set -g status-interval 1
# scratch popup を開いて数分使う
```

- **再現する** → 原因は「毎秒の再描画 × popup overlay」。対策は下の (A) か (B)
- **再現しない** → 原因は `terminal-features` の膨張 (38 エントリ) だった可能性が高い。
  すでに修正済み (`5f12aea`) なので追加対応は不要。この issue は done へ

## 実測で分かっていること (2026-08-21)

### tmux 3.7b が数える表示幅 (隔離サーバで cursor_x を実測)

| 文字 | 用途 | tmux の幅 |
|---|---|---|
| ⚡ 🔍 🔔 🚀 | status-left の島 / 検索 HUD / agent 状態 | 2 |
| ◐ ◓ ◑ ◒ | status-left のスピナー (毎秒 4 相) | 1 |
| ⚙ ✓ ● | agent 状態 (East Asian Ambiguous) | 1 |

### 幅の不一致は原因ではない (仮説を実測で棄却)

第一候補は「端末が Ambiguous を全角で描き、tmux が半角と数えて 1 セルずれる」だったが、
Terminal.app の既定プロファイル "Claude Warm" は **`EastAsianAmbiguousWide = 0`** (実測) =
幅 1 で描くため tmux と一致している。この経路は原因ではない。

### popup overlay の既知バグも該当しない

tmux issue #4920 (popup を閉じた後に枠が ~1 秒残る) は 3.5a〜3.6b の問題で **3.7 で修正済み**。
稼働は 3.7b で、repo 側の回避策も撤去済み (`docs/tmux-as-platform.md` に記録あり)。

### 残る候補: 毎秒の再描画 × overlay × nested tmux

規模を数えると毎秒書き換わる範囲が大きい:

- window 95 個 / client 幅 235 桁
- 毎秒変化する format 要素が 13 箇所 (点滅 2 相 / スピナー 4 相 / 放置フェードの段)
- scratch popup は**本物の tmux セッションを attach する nested 構成**で、内側も
  `status 2` (2 行) を毎秒描く = 端末への書き込みが二重

popup が乗っている間、tmux は popup 領域を避けて背景を**部分再描画**する。この頻度が高いほど
取りこぼしが見える、という筋。`status-interval 0` でブレが消えたのなら、この線が濃い。

### 未検証

- **bare emoji (⚡ U+26A1 など VS16 なしの形) の幅**。tmux は 2 と数えるが、Terminal.app が
  「テキスト表示」として 1 セルに描くと 1 セルずれる。これは**画面を見ないと判定できない**
  (Claude 側からは観測不能)
- 素の端末 (tmux を介さない) での挙動

## 対策案 (再現した場合)

### (A) popup 表示中だけ status の更新を止める

演出を残したままブレだけ消せる第一候補。骨格:

```tmux
set-hook -g client-attached 'if -F "#{==:#{session_name},scratch}" "set -g status-interval 0"'
set-hook -g client-detached 'set -g status-interval 1'
```

🚨 `client-attached` は popup 以外の attach でも発火するので条件を詰める必要がある。
実装するなら隔離サーバ (`-L`) で検証してから入れること。

### (B) `status-interval 0` を既定にする (演出を捨てる)

ブレない方を優先するなら、`_tmux.conf` の `status-interval` を 0 に固定し、**毎秒更新前提の
仕組み (点滅の 2 相・スピナーの 4 相・fade の段) を整理する**。中途半端に残すと「動かない演出」
を次の人が直そうとするため、捨てるなら定義も畳む。

### (C) 幅が揺れうる文字を status から外す

未検証の bare emoji が原因だった場合の対策。`#{p18:...}` の固定幅パディングは既に使われて
いるが、中の文字幅がずれると桁は守れない。

## 決着 (2026-09-01) — ユーザー判断で done。実測すると切り分け条件は既に成立していた

ユーザー判断 (2026-09-01) で done へ送る。閉じるにあたって現在値を実測した:

| 実測 | 値 | 出典 |
|---|---|---|
| `tmux show -g status-interval` (稼働サーバ) | **1** | 2026-09-01 実測 |
| `_tmux.conf` の設定 | **1** | `_tmux.conf:73` |
| `tmux show -s terminal-features` の行数 | **5** | 2026-09-01 実測 (`5f12aea` 反映済み) |

🚨 **この issue が「切り分けのために戻せ」と書いていた条件 (`status-interval 1`) は、実は
一度も外れていない**。`status-interval 0` は調査中の一時的な set であって `_tmux.conf` には
入っておらず、稼働サーバも 1 のままだった (この節の初稿は「現状 0」と書いていたが誤り。
実測で訂正した)。

つまり**現在の稼働状態がそのまま「人にやってほしいこと」の実験条件**になっている:

- 毎秒の再描画は動いている (`status-interval 1`) = 対策 (A)(B) は入っていない
- `terminal-features` の膨張だけが直っている (38 → 5 エントリ、`5f12aea`)

この条件でブレが出ていないなら、issue の判定表どおり**原因は `terminal-features` の膨張**
だったことになり、追加対応は不要。🚨 ただし「今ブレていないか」をユーザーに確認して
いないので原因は断定しない (「再現していないという報告も取っていない」が正確な状態)。

- **再開の trigger**: 背景のブレが再発したとき。そのときは (A)(B)(C) から着手する。
  着手前に `status-interval` と `terminal-features` の実測値を採ること (この issue が
  実測なしの前提のまま 10 日走ったことが、上の誤りの原因)
- 未検証のまま残るもの: bare emoji (⚡ U+26A1 等) の実描画幅 (画面を見ないと判定できない) /
  素の端末での挙動

## 関連

- `5f12aea` — `terminal-features` の reload 膨張を止めた commit (38 → 5 エントリ)。
  今回のブレと同時期の変更なので、切り分けの対象
- `src/glogx/width.go` の冒頭 — 同型の症状 (「毎秒の再描画のたびに桁がずれてガタつく」
  Terminal.app + tmux、ユーザー報告 2026-07-24) と、その原因が幅モデルの食い違いだった記録。
  今回は tmux 側なのでライブラリ統一という解は使えないが、症状の出方は同じ
- `docs/tmux-as-platform.md` — popup / display-menu のバージョン要件と #4920 の記録
