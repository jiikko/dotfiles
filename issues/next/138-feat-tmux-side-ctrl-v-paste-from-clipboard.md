# 138 feat: 大量ペーストの文字化け対策として、C-v を tmux 側で奪ってクリップボードを直接流し込む

起票日: 2026-08-29
種別: feat
関連: [094](094-human-verify-av1ify-clipboard-input.md) / [095](095-retro-av1ify-clipboard-input-2026-08-22.md)（同じ「貼り付けで文字が落ちる」問題への別解）

## 背景（問題提起）

**ターミナルへ大量の文字を貼り付けると、途中で文字が落ちて不完全な状態になることがある。**
`av1ify` のクリップボード入力（`zshlib/_av1ify.zsh`）は、まさにこの問題を回避するために
「シェルの行編集を経由せず `pbpaste` から直接読む」形にしてある（同ファイルのコメントが正本）。

ただしそれは **av1ify という 1 コマンドだけの回避**であり、素の `git commit -m ...` や
長い URL・長いパスの貼り付けには効かない。**入力手段そのものを直したい**、というのが本 issue。

## やりたいこと

zsh のプロンプト上で **`C-v` を tmux 側で奪い**、tmux プロセスが `pbpaste` を読んで
`paste-buffer` でペインへ直接流し込む。端末エミュレータ → tmux のキー入力経路を通さない。

**zsh のときだけ**にしたい（nvim の矩形選択・zsh の `quoted-insert` など、既存の `C-v` を潰さない）。

## 提案する形（案 A）

```tmux
# _tmux.conf
bind -n -N "クリップボードを直接ペースト (zsh のみ・prefix なし)" C-v \
  if-shell -F '#{==:#{pane_current_command},zsh}' \
    'run -b "pbpaste | tmux load-buffer -b ctrlv - && tmux paste-buffer -d -p -b ctrlv"' \
    'send-keys C-v'
```

- `#{pane_current_command}` はそのペインの**フォアグラウンドプロセス名**なので、nvim/less/ssh が
  前面にいる間は `send-keys C-v` に落ちて既存挙動が残る
- `paste-buffer -p` で bracketed paste を付ける（付けないと複数行が改行ごとに即実行される）
- `-b ctrlv` + `-d` でバッファ名を固定して使い捨てにし、tmux のバッファスタックを汚さない
- `run -b` でバックグラウンド実行（`pbpaste` が固まっても tmux サーバを止めない）
- `_tmux.conf` の既存 bind に倣い `-N` の説明を必ず付ける（キーガイドに出す）

## 🚨 先に観測すること（着手前の必須ステップ）

**「どこで文字が落ちているか」がまだ特定できていない。** 案 A が効くのは
**端末エミュレータ → tmux の入力経路**で落ちている場合だけで、
**tmux → pty → zle（zsh の行編集）** 側で落ちているなら案 A では直らない。

- 再現条件を作る（何文字くらいから / 改行を含むか / 特定の端末だけか / tmux なしでも起きるか）
- 最低限の切り分け: ①tmux なしの素の zsh ②tmux 内 zsh ③tmux 内で `cat > /tmp/x`（zle を通さない）
  の 3 通りで同じ長文を貼り、どこから壊れるかを見る
- ③でも壊れるなら zle は無関係（端末 → tmux 経路）。②だけ壊れるなら zle / bracketed-paste-magic 側

観測なしで案 A を入れて「直った気がする」で閉じない。

## 代替案（観測結果しだいでこちらが正解になりうる）

| 案 | 形 | 効く範囲 |
|---|---|---|
| A | tmux で `C-v` を奪い `paste-buffer` | 端末 → tmux の入力経路で落ちている場合 |
| B | zsh の zle widget で `LBUFFER+="$(pbpaste)"` | **pty を一切通らない**ので、①②③のどこで落ちていても効く。ただし zsh 専用で、tmux の有無に関係なく動く |
| C | 現状維持（av1ify 方式をコマンド側に足す） | そのコマンドだけ |

**B は「zsh のときだけ」という要件を構造的に満たす**（zle は zsh のプロンプト上でしか動かないので、
`pane_current_command` のような当て推量の判定が要らない）。A より素直な可能性が高いので、
観測の前に A へ決め打ちしない。

## 未確定・注意点

- `pbpaste` は **tmux サーバが動いているマシン**のクリップボードを読む。SSH 先の tmux では
  期待どおりにならない（その場合は OSC 52 の話になり、本 issue の範囲外）
- `bind -n` は root テーブルなのでペイン内のアプリにキーが一切届かない。`if-shell` の判定が
  外れたときに `send-keys C-v` でちゃんと戻せているかは実測が要る
- zsh 側の bracketed paste（`bracketed-paste-magic`）が有効かどうかで、長文ペーストの体感速度が変わる。
  大量行で遅いなら magic 側を切る判断もありうる
- tmux は 3.7（`.tmux-version`）。`load-buffer -` の標準入力読みはこのバージョンで可

## 受け入れ条件

- [ ] 壊れる箇所を①②③の切り分けで特定し、結果を本 issue に書く
- [ ] 特定結果に基づいて A / B / C を選ぶ（選ばなかった案は「なぜ効かないか」を 1 行残す）
- [ ] 実装した経路で、既存の `C-v`（nvim の矩形選択・zsh の `quoted-insert`）が壊れていないことを確認する
- [ ] tmux 側を触るならテストは `tests/tmux/` の socket 隔離方式に倣う。zle 側を触るなら
      挙動確認は pty driver（`_claude/rules/verify-interactive-prompt-with-pty-driver.md`）
- [ ] 人手でしか見られない部分（実際の長文ペーストが壊れないか）は `human` issue に切り出す
