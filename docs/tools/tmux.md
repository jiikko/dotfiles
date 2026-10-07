# tmux のキーと表示

prefix は `C-t`。設定の正本は [_tmux.conf](../../_tmux.conf)（コメントに各設定の意図と経緯を記載）。

## ペイン操作

| キー | 動作 |
|---|---|
| `C-t v` / `C-t \|` | 左右に分割（カレントパス引き継ぎ） |
| `C-t s` / `C-t -` | 上下に分割（同上） |
| `C-t h/j/k/l` | ペイン移動（`C-h/j/k/l` でも可。**repeat は付けていない** — 付けると repeat-time 内にシェルへ打った h/j/k/l が移動に化ける） |
| `C-t o` / `C-t C-o` | 次のペインを選ぶ |
| `M-h/j/k/l` | prefix なしでペイン移動（端でループしない） |
| `C-t H/J/K/L` | リサイズ（連打可） |
| `C-t z` | ズーム（ズーム中はペイン境界に 🔍 ZOOM と解除ヒントが出る） |
| `C-t x` | ペインを kill（**画面中央に gum の確認ダイアログ**。要 `brew install gum`） |
| `C-t q` | 自分以外の全ペインを kill（同上、誤爆防止でデフォルトは「やめる」） |

## ペインの入れ替え・移動

| キー | 範囲 | 動作 |
|---|---|---|
| `C-t e` → 数字 | 同一 window | ペイン番号オーバーレイから選んで現在ペインと交換 |
| `C-t G` | window 跨ぎ | 現在のペインを fzf popup で選んだ window へ送る (give)。get 側の旧 `C-t g` は glogx の popup に転用済み |
| `C-t !` | — | ペインを独立した window に切り出す (break-pane) |

## ウィンドウ操作・ジャンプ

| キー | 動作 |
|---|---|
| `C-t c` / `C-t C-c` | 新規 window（カレントパス引き継ぎ） |
| `C-t Space` / `C-t BSpace` | 次 / 前の window（`M-n` / `M-p` なら prefix なし） |
| `C-t Tab` | 直前にいた window とトグル（claude 窓 ⇄ 作業窓の往復用） |
| `C-t f` | **fzf popup** で全セッションの window を曖昧検索してジャンプ（プレビュー付き） |
| `C-t w` | choose-tree（標準のツリー画面） |
| `C-t <` / `C-t >` | window メニュー / pane メニュー（Swap・Kill・Rename 等） |
| `C-t u` | 最後に作業した window へジャンプ |
| `C-t a` / `C-t A` | エージェント常駐パネルの表示トグル / エージェントのペインへ fzf でジャンプ |
| `C-t &` | 今の window を kill（確認あり） |

## popup・その他

| キー | 動作 |
|---|---|
| `C-t g` / `C-g` | **git log TUI (glogx) の popup**（`C-g` = Ctrl+g なら prefix 不要。popup 内でもう一度 `C-g` で閉じる）。いま見ているペインの cwd の repo を開く（nvim や Claude のペインからでも効く。repo の外ではトーストで知らせる）。status viewer (`s`) で stage / unstage、`b` で push。キー操作は [src/glogx/README.md](../../src/glogx/README.md) |
| `C-t m` / `C-t Enter` | **予約入力**: N 時間 M 分後にこのペインへ文字列を送る (一覧・取消も)。[src/schedkeys](../../src/schedkeys/README.md) の popup |
| `C-t t` | **スクラッチターミナルのトグル**。専用セッション scratch をフローティング表示し、popup 内でもう一度押すと閉じる（セッションは生きるので作業状態は保持） |
| `M-[` | prefix なしでコピーモードへ。vi キーバインド、`v`/`Space` で選択開始、`y`/`Enter` で pbcopy にコピーして抜ける |
| `C-t y` / `C-t C-y` | 画面から URL・パス・単語を fzf で選んで吸い出す |
| `C-v` | prefix なしでクリップボードを直接ペースト（zsh のペインだけ） |
| `C-t M-c` | 全ペインのスクロールバックを解放（確認あり） |
| `C-t R` | 設定リロード |
| `C-t C-s` / `C-t C-r` | レイアウトの手動保存 / 手動復元（確認あり）（tmux-resurrect。自動保存・復元は continuum + 独自 hook で常時動作） |

## 視認性まわり

- アクティブペインの境界は **緑の発光帯**（fg=bg 塗りつぶし + 二重線 + 矢印インジケータ。copy-mode 中は黄、synchronize-panes 中は赤）
- カーソルは **緑の明滅ブロック**（カーソルはアクティブペインにしか無いので現在地の点光源になる）
- 各ペイン上端にタイトルバー（ペイン番号 + パス + 実行コマンド。アクティブは緑の帯）
- スクロール位置バー (`pane-scrollbars`) は off（tmux 3.6a ではバーを出したペインが 1 列狭まって reflow が起きるため）

## ウィンドウ名の自動反映

ウィンドウ名は「**アクティブペインのタイトル (pane_title) に自動追従**」する構成。

```
各ペイン内のプログラムが OSC 2 で pane_title をセット
  ├─ zsh: preexec/precmd が実行コマンドの表示名をセット (zshlib/_tmux_window_name.zsh)
  │       コマンド名 → アイコン付き表示名のマッピングは zshlib/tmux-window-name.yaml
  ├─ nvim: 編集中のファイル名を自身でセット
  └─ Claude Code: セッションの topic を自身でセット
        ↓
tmux の automatic-rename-format '#{pane_title}' が
アクティブペインのタイトルをウィンドウ名に反映 (_tmux.conf)
        ↓
ステータスバーでは #{=15:window_name} で 15 文字に切り詰めて表示
(ウィンドウ名自体・ペイン境界の表示はフルのまま)
```

- ウィンドウ名を直接リネームする `\033k` エスケープは `allow-rename off` で遮断している。
  旧構成 (zsh が `\033k` でウィンドウ名を直接書き換え) では「最後にプロンプトを出した
  ペイン」が非アクティブでもウィンドウ名を奪う事故があった (split 直後に zsh へリセット等)。
  タイトルはペイン単位の OSC 2 に一本化し、ウィンドウ名への昇格は tmux 側に任せる
- 既に起動中の zsh は古い zshlib を読んだままなので、挙動を反映するには `exec zsh` が必要

## Claude Code の作業状態表示

Claude Code を動かしているペインの境界に作業状態が出る。

| 表示 | 色 | 意味 |
|---|---|---|
| `⚙ working` | 黄 | 応答処理中 |
| `⚙ working (bg:N)` | 黄 | バックグラウンドタスク N 件の完了待ち (手は空いていない) |
| `🔔 input` | 赤 | permission 承認待ち・質問への回答待ち (承認すると working に自動復帰) |
| `🔕 seen` | 灰 | 入力待ちの window を開いて見たが、まだ応答していない (応答すると working へ戻る) |
| `✓ idle` | 緑 | 完了・次の指示待ち |

さらに `🔔 input` / `✓ idle` への遷移時、**そのペインがどのクライアントでも前面に
見えていない**なら macOS 通知センターに通知が飛ぶ (input は音あり、idle は音なし)。

`⚙ working (bg:N)` の間は、ベルも通知も出ないし `🔔` にもならない。bg タスクの完了を
待って止まっているだけで、人がやることが無いため (入力待ちの催促 `idle_prompt` が
来ても抑える)。bg が片付いて Claude が動き出せばフラグは落ち、そのあとの承認待ちは
従来どおり鳴る。
複数 claude 並走時の手待ち検知が画面監視なしでできる。
同じアイコンはステータスバーのウィンドウリストにも出るため、別ウィンドウの claude の
状態も一覧できる。

- Claude Code の hooks (`_claude/settings.json`) が `_claude/hooks/tmux-pane-state.sh` を呼び、
  ペイン単位オプション `@claude_state` (と bg 待機フラグ `@claude_bg`) を出し入れする。
  ツール呼び出しのたびに走る working だけは、bash を起こさない `_claude/hooks/tmux-pane-working.sh` を直接呼ぶ
- `_tmux.conf` の `pane-border-format` が `#{?@claude_state,...,}` で表示。未設定ペイン
  (通常シェル) には何も出ない。セッション終了 (SessionEnd) で自動クリア
