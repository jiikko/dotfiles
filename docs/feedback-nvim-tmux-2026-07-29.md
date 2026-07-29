# nvim / tmux 設定・コードのレビューフィードバック (2026-07-29)

nvim 側 (_nviminit.lua + nvim/lua/dotfiles/* + ftplugin/*、約 2,000 行) と tmux 側
(_tmux.conf + scripts/tmux_* + zshlib/_tmux_* + vendor ローカルパッチ、約 2,600 行) を
全読 + 実測 (bench_nvim.sh / startuptime / docker 上の tmux 3.4 再現) でレビューした
結果の所感。個別の修正は各 commit に、ここには**構成全体への評価と、事故から一般化
できる弱点パターン**を残す。

## 総評: 強み (維持すべき性質)

- **意図の文書化が「誤指摘の即時棄却」レベルで機能している**。「検討済み却下」「⚠️ 罠」
  コメントの密度が高く、外部レビュー (エージェント 2 体 + 本体) の指摘候補の大半が
  コメント参照だけで棄却できた。素朴なリファクタ提案が空振りする構成は健全。
- **テストがモジュール境界と 1:1 に対応している** (folds ⇄ test_folds_timer、
  smooth_scroll ⇄ test_smooth_scroll、toast ⇄ test_tmux_toast の stub 方式)。境界が
  正しく引けている証拠であり、今回のような境界単位の修正が安全にできた。
- **ホットパスの設計原則が一貫している**: zsh hook の REPLY 契約 (fork ゼロ)、fold の
  expr→manual 凍結 (バッファ切替 690ms→17ms)、smooth-scroll の押しっぱなし素通し。
  実測でも nvim スクロールのオーバーヘッドは +19µs/打 (知覚閾の 3 桁下) に収まっている。
- **防御機構が実戦で機能している**。resurrect 保存の Fix B 退行ガードは、レビュー作業中に
  サンドボックスから撃たれた空ダンプ保存 (0 window) を 2 回とも実際にブロックした。
  「保存内容の健全性はガードが見る」という設計が正しかった実証例。
- **リソースリークは検出ゼロ** (nvim 全域)。uv timer の stop+close 徹底・buffer-local
  autocmd の自動掃除・BufWipeout での map 掃除など、過去事故の対策がパターンとして
  横展開されている。

## 事故から一般化した弱点パターン (今後の設計判断に効かせる)

### 1. hook + run-shell はエラーの排出先が「アクティブ pane の view-mode」

toast 事故 (b166fdf) の一般化。`set-hook` から呼ぶスクリプトが非 0 / stderr / stdout を
返すと、tmux はそれをアクティブ pane に view-mode として積む。tmux 3.4 ではモード
スタックが copy-mode スクロールを無反応にする実害まで連鎖した。
**hook から呼ばれるスクリプトは「表示先が無い・依存が無い環境では無音の exit 0」を
契約にする** (通知系は特に。エラーを返すのが誠実に見えて、hook 文脈では pane を汚す
方が害が大きい)。tmux-toast にはこの契約をテストで固定済み。新しい hook スクリプトを
足すときは同じ契約を要求すること。

### 2. 同名 hook の 2 本目は index を振る (index なしは [0] 上書き)

toast が debounce-save に 5 日間 silent に消されていた事故 (2b8376f)。`show-hooks -g` に
出ない pane-* hook では気づく手段が無い。規約は _tmux.conf ヘッダに昇格済みだが、
**「機能 A と機能 B が別々の時期に同じ hook を取り合う」構図自体が再発源**なので、
hook を足す変更では既存の同名 hook を必ず grep すること。

### 3. 状態の永続化経路は「一度壊れた状態」も忠実に保存し続ける

window 名固着の真因 (2026-07-27 対応)。旧実装 (\033k) が残した per-window
automatic-rename off を resurrect が世代を跨いで復元し続け、実装を直した後も症状だけが
再発し続けた。**挙動を移行するコミットは、live サーバ / 保存ファイルに残った旧状態の
掃除までがスコープ** (conf と zsh を直しただけでは「新規 window だけ直る」)。
resurrect のように状態を焼き込む機構がある場合、移行 checklist に「保存側の除染」を
含めること。

### 4. ローカル (tmux 3.7b/macOS) と CI (tmux 3.4/ubuntu) は「エラーの見せ方」で分岐する

機能差 (floating panes の有無等) は既にガードされているが、今回の事故はどちらも
「同じエラーをどう表面化するか」の差で顕在化した (view-mode 積み / display-message の
制御文字エスケープ)。**CI が 3.4 を使い続ける限り、tmux 系の不可解な CI 失敗は
docker 一発で再現するのが最短**:
`docker run --rm -v ~/dotfiles:/dotfiles:ro ubuntu:24.04` + apt で tmux/zsh/perl を
入れて該当テストを叩く (今回この手順で 3 プローブ以内に真因へ到達)。app 層ログの推測より
先にこれを回すこと (instrument-before-second-fix の tmux 版)。

### 5. fallback 経路は「コメントの主張」ごとテストする

fzf window picker の column 不在劣化 (1a6a80d) は、コメントが「失われるのは桁揃えだけ」と
主張したまま実際は表示情報の大半が消えていた。fallback は本線環境では一度も踏まれない
ため、**「劣化はここまで」という主張はその環境を再現するテスト (PATH から道具を抜く等) が
無い限り信用しない**。picker には column 不在ケースを追加済み。

## 残っている改善候補 (未対応。着手時の参考)

- **fzf_pane_move / fzf_jump の機能テスト不在**: 共有ロジック (window_picker) は厚く
  テスト済みだが、`join-pane` / `switch-client` を呼ぶ末端は静的 grep 検査のみ。
- **terraformls の on_attach 上書き**: nvim 0.12 へ上げたら削除する (lsp.lua の該当
  コメントが削除条件を持つ。現行 0.11.5 では必要)。
- **lualine の git root 解決** (`vim.fs.root`) は statusline 更新ごとに上方向 stat 走査
  (数十 µs)。バッファ毎キャッシュは無効化の縁ケースが増えるため**見送りと判断済み**。
  statusline が遅いと感じたときの再評価ポイントとしてだけ記録。
- **resurrect 旧世代の automatic-rename 除染** は 424 世代へ適用済み。除染前の
  バックアップは `~/.local/share/tmux/resurrect/backup-before-autorename-scrub-20260727.tar.gz`
  (不要になったら消してよい)。
- **launcher セッションの named window** (`new-window -n`) は意図的な命名で、
  automatic-rename off が正しい状態。window 名固着の調査で「off = 異常」と一律に
  扱わないこと。

## 実測値の記録 (2026-07-29 ローカル、比較の基準用)

| metric | フル config | --clean | 備考 |
|---|---|---|---|
| nvim startup (min-of-5) | 105ms | 20ms | 内訳最大は lazy.setup 一式 ~29ms。突出した 1 点なし |
| bufload 5000 行 | 69ms | 8ms | treesitter 同期パース + fold 凍結の初回一括払い |
| scroll j ×2000 | 75ms | 37ms | +19µs/打 |
| buf_switch ×200 | 26ms | 6ms | fold 凍結の見返り (130µs/回) |

起動・スクロールとも体感に乗る遅さは無し。eager ロードの treesitter / matchup は
作者が遅延ロード非対応と明言しているため対象外 (spec コメントに記録済み)。
