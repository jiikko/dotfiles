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

- [x] 切り分けの結果を本 issue に書いた (2026-09-04。下記。①は人の貼り付けが要るので未再現)
- [x] A を選んだ (2026-09-04。下記に B / C を採らなかった理由)
- [x] 判定式が zsh 以外のペインで偽になることをテストで固定した (実キー押下の確認は human issue へ)
- [ ] tmux 側を触るならテストは `tests/tmux/` の socket 隔離方式に倣う。zle 側を触るなら
      挙動確認は pty driver（`_claude/rules/verify-interactive-prompt-with-pty-driver.md`）
- [ ] 人手でしか見られない部分（実際の長文ペーストが壊れないか）は `human` issue に切り出す

## 観測の結果 (2026-09-04)

隔離した tmux サーバ (`-L`。`$TMUX` は unset) で①②③を切り分けた。

| 経路 | 結果 |
|---|---|
| ③ tmux → pty → `cat` (zle を通さない) | 24200 バイト送って **24200 バイト。欠落なし** |
| ② tmux → pty → zsh の zle (`zsh -f`) | 24000 バイトを 1 行で送って **24001 (= 24000 + 改行)。欠落なし** |
| ② + `bracketed-paste-magic` 有効 | 同じく **24001。欠落なし** (widget が有効だった証拠: `zle -l` に `bracketed-paste (bracketed-paste-magic)`) |
| ① 端末エミュレータ → tmux | 🚨 **未再現**。人が実際に貼り付けないと作れない |

つまり **tmux から下流は 1 バイトも落とさない**。落ちるとすれば端末 → tmux の経路で、
そこを迂回する **案 A が効く場所**にあたる。

- **案 B (zle widget で `LBUFFER+="$(pbpaste)"`) を採らなかった理由**: B は「どこで落ちても効く」
  が、②③が無傷と分かったので **B が余分に救う範囲は今回の症状に無い**。加えて B は zsh 専用で、
  tmux の外でも C-v の意味を変えてしまう
- **案 C (av1ify 方式をコマンド側に足す) を採らなかった理由**: 1 コマンドしか救わない (issue の前提)

## 実装 (案 A) と、提案からの変更点

```tmux
bind -n -N "クリップボードを直接ペースト (zsh のみ・prefix なし)" C-v \
  if-shell -F '#{==:#{pane_current_command},zsh}' \
    'run -b "pbpaste | tmux load-buffer -b ctrlv - && tmux paste-buffer -d -p -b ctrlv -t #{pane_id}"' \
    'send-keys C-v'
```

🚨 **提案に無かった `-t #{pane_id}` を足した**。入れ子の `tmux paste-buffer` は run-shell の
対象ペインを継承せず「**今アクティブなペイン**」へ貼る (実測: `run -t zsh` で走らせても別セッションの
ペインに入った)。単一クライアントなら押したペインが active なので偶然当たるが、
**複数クライアントが別ペインを見ていると誤爆する**。

## テスト (`tests/tmux/test_ctrl_v_paste.sh`)

socket 隔離 (`unset TMUX` + ユニークな `-L`) で、`pbpaste` を stub に差し替えて実クリップボードに触らない。

🚨 **キー押下そのものは自動テストで再現できない**。`tmux send-keys` は key table を通さずペインへ
直接キーを送るので、root テーブルの bind は発火しない (実測: zsh に生の C-v が届いて `^` が出た)。
そこで **bind の登録 / 判定式 / true 側のコマンド**の 3 つに分解して固定した。

⚠️ **判定式とコマンドは conf に登録された bind から取り出して評価する**。テスト側に式をコピーすると、
bind の条件を `if-shell -F '1'` に書き換えて全ペインから C-v を奪っても**テストが緑のまま通る**
(2026-09-04 に実際に踏んだ)。

変異検証:

| 変異 | 結果 |
|---|---|
| bind ごと削除 | **red** (登録数 0 / 説明なし / コマンドを取り出せない) |
| 判定を `if-shell -F '1'` にする (全ペインで C-v を奪う) | **red** (判定式の 2 項目。⚠️ 抽出を直す前は**全項目 green だった**) |
| `-t #{pane_id}` を外す | **red** (別ペインへ貼られる) |

## 残っていること

- **① 端末 → tmux の欠落は未再現**。この bind が実際に症状を消すかは**人が貼って確かめる**必要がある
- 実キー押下での分岐 (zsh で奪う / nvim で `send-keys C-v` に戻る) も同じく人の確認が要る

