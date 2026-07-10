# tmux 設定監査 (2026-07-10) — 検証済みバグ 10 件

自作 tmux コード全域 (_tmux.conf 676 行 / zshlib/_tmux_session.zsh / zshlib/_tmux_window_name.zsh /
scripts/tmux_*.sh / scripts/lib / _claude/hooks / tests) を 9 グループ並列で監査し、
各指摘を「反証デフォルト」の敵対的検証 (専用ソケット `tmux -L audit_*` での実プローブ含む) に
通した結果。**検証を通過したもののみ**起票している (発見 12 件中 2 件は検証で棄却、末尾に記録)。
検証環境: tmux 3.7b / macOS。codex レビューは使用禁止指示のため、敵対的検証がその代替。

- 検証状態の凡例:
  - ✅ **プローブ実証**: 専用ソケットで実挙動を再現済み
  - 📖 **コード確定**: 機序をコードで確定 (実行プローブは未実施)

---

## P2

### 1. @tt-adopted が再起動を跨いで消え、復元された adopted hold を GC が kill する

- **場所**: `zshlib/_tmux_session.zsh` (`_tt_gc_stale_holds` / adopt 経路の `set-option @tt-adopted`)
- **検証**: ✅ プローブ実証 (save→restore→GC の end-to-end を隔離環境で再現)
- **機序**:
  1. degraded boot (rc=1/2) で `_tt_impl` が hold を adopt し `@tt-adopted 1` を立てる (e7bb0d7 の保護)
  2. `@tt-adopted` は**セッションオプションで、tmux-resurrect の保存フォーマットに含まれない**
     (vendor save.sh の save_all は panes/windows/state/grouped のみ dump。restore.sh が set-option
     するのは window の automatic-rename だけ)
  3. adopted hold は実セッションとして last に保存される (他に実セッションが併存する場合。
     `tt_only_hold_sessions` は「hold 以外が皆無」のときしか保存抑止しない)
  4. 次 boot の tt: 復元 rc=0 → `_tt_gc_stale_holds` が **attach より先に**走る。復元された
     `__tt_hold_<旧pid>` はフラグ不在・旧 pid 死亡・未 attach・1win/1pane で全ガードを素通りし kill
  - `@resurrect-capture-pane-contents on` で復元されたスクロールバック・cwd・レイアウトが破棄される。
    GC が同一 tt 呼び出し内で attach より先に走るため `tt __tt_hold_<pid>` でも救えない
- **プローブ**: 隔離環境で `__tt_hold_88888` + `@tt-adopted=1` → vendor save.sh 実行 → dump に
  hold は入るが "tt-adopted" は 0 件 → 新サーバで restore → フラグ空で復元 → 実物の
  `_tt_gc_stale_holds` を実行 → hold のみ kill された (併存セッションは残存)
- **姉妹経路 (同根)**: adopted hold が唯一のセッションだと `tt_only_hold_sessions`
  (scripts/lib/tmux_resurrect_guards.sh) が保存自体を抑止するため、reboot で丸ごと消える。
  どちらも「adopt = 実作業セッション化」を hold 名 + runtime フラグでしか表現していないのが根因
- **修正方向 (構造)**: adopt 時にセッションを実名 (`$name`) へ **rename して hold 名前空間から出す**。
  GC 対象からも保存抑止からも構造的に外れ、フラグの永続化問題自体が消える。
  (フラグを resurrect の保存対象に足す方向はパッチワークで、vendor 改変も要るため非推奨)
- **補足 (プローブで発見)**: `set-option -t "=name"` / `show-options -t "=name"` は tmux 3.7b で
  `no such session: =name` になる (`"=name:"` なら成功)。has-session/kill-session の `=name` は成功。
  現コードの `-t "=$hold"` 系が実際にどの経路で通っているかは修正時に要確認

## P3

### 2. bind e (ペイン交換) が marked pane 存在時に「現在ペインと交換」にならない

- **場所**: `_tmux.conf:422` — `bind e display-panes -d 0 'swap-pane -t %%'`
- **検証**: ✅ プローブ実証 (3.7b 実挙動 + man 記載確認)
- **機序**: `swap-pane` は `-s` 省略時、**marked pane があればそれを source に使う** (man 明記)。
  既定の `prefix m` (select-pane -m) は unbind されていないため、誤爆で一度 mark すると以後の
  C-t e は「選んだペイン ↔ marked」を交換し続ける。mark は sticky かつこの設定では表示手段が無い
  (pane-border-format / window-status に `#{pane_marked}` なし) ので気づけない。
  意図コメント (416 行「そのペインと現在ペインが交換される」) に反する
- **修正**: `swap-pane -s %%` に変える (-s 明示で marked の default 解決が無効化され、destination が
  現在ペインになる)。プローブで mark 存在下でも意図どおり動くことを確認済み。
  注意: -s 版は交換後のフォーカス追従が変わるため `-d` の要否を合わせて確認

### 3. run-shell への format 展開が sh のダブルクォート任せ (quoting 穴)

- **場所**: `_tmux.conf:468-469` (launcher menu) / `495,497` (bind t / C-t scratch)
- **検証**: ✅ プローブ実証
- **機序**: run-shell は format 展開後の文字列を /bin/sh に渡す。`"#{session_name}"` /
  `"#{pane_current_path}"` はダブルクォート内展開なので、値に `"` を含むと sh 構文エラーで
  **無反応** (rc=2、スクリプト未着火をプローブで確認)、`$` や backtick は sh が展開する
  (`x$HOMEy` → argv=[x] を確認。理論上は任意コマンド混入)。tt のセッション名は
  `basename "$PWD"` 由来で `_tt_sanitize_session_name` は `.` と `:` しか置換しない
  (zshlib/_tmux_session.zsh:38) ため、ディレクトリ名にこれらの文字があると発火する
- **修正方向**: run-shell に生値を埋めず、**スクリプト側で `tmux display -p` により
  client/session/path を解決する** (bind x/q/M-c の popup が既に採っている方式に揃える)

### 4. tmux_version_gte.sh の無引数経路がバージョン接尾辞 (3.7a 等) で常に false になる

- **場所**: `scripts/tmux_version_gte.sh` (req 側の正規化) — 現在は潜在バグ
- **検証**: ✅ プローブ実証 (`.tmux-version`="3.7b" で `test: 7b: integer expression expected` → 常に不足扱い)
- **機序**: サーバ版数 `v` は `sed 's/[^0-9.]//g'` で接尾辞を剥がすのに、`.tmux-version` から読む
  `req` は `tr -d '[:space:]'` のみ。`.tmux-version` を tmux の標準命名 ("3.7a" 等) に更新した瞬間、
  `req_min="7a"` が数値比較に渡りエラー → `_tmux.conf` 冒頭の全体バージョンゲートが
  **サーバが要求を満たしていても** false になる
- **傍証**: `tests/tmux/test_version_gte.sh:12` のコメントは「要求版数の抽出はサフィックス除去を含む」と
  実装と矛盾した記述になっている (テストコメントも直す)
- **修正**: req 側にも `sed 's/[^0-9.]//g'` を適用する (1 行)

### 5. check_syntax.zsh が HOME を隔離せず、実観測ログに偽エントリを書き込む

- **場所**: `scripts/check_syntax.zsh` (tmux 検査の起動行) × `_tmux.conf:596` (観測フック)
- **検証**: ✅ プローブ実証 (隔離 HOME で同一起動 → conf-source 行が書かれることを確認)
- **機序**: `_tmux.conf:595` は「HOME 隔離のテストは temp HOME に書くため実ログを汚さない」という
  契約でフック (`>> "$HOME/.cache/tt-restore-trigger.log"`) を置いている。tests/tmux/*.sh は全て
  HOME を隔離しているが、check_syntax.zsh (make test-syntax、`make test` に含まれる) だけ
  TMUX_TMPDIR しか隔離せず、実 HOME のまま _tmux.conf 付きでサーバを起動する → 実行のたびに
  実ログへ偽の `conf-source tmux_procs=N last=...` 行が入る。このログは restore 不発調査専用の
  観測装置なので、偽エントリは次の調査を誤導する (instrument の目的を毀損)
- **修正**: check_syntax.zsh の tmux 起動に `HOME=$tmux_tmpdir/home` (+ XDG_DATA_HOME) を
  tests/tmux/test_tmux.sh:24 と同様に付ける。DOTFILES_DIR の明示固定も同テストの注意書きどおり必要

### 6. launcher の `has-session || new-session` が set -e 下でレースに負けると無言死する

- **場所**: `scripts/tmux_launcher_run.sh:34` (`set -eu` は 20 行目)
- **検証**: 📖 コード確定 (set -e 挙動は `sh -c 'set -e; false || false; echo x'` で実証済み。
  2 プロセス同時レース自体の再現は未実施)
- **機序**: `tmux has-session -t "$sess" || tmux new-session -d -s "$sess"` は、並行起動された
  2 プロセスが両方「未存在」判定 → 片方の new-session が `duplicate session` で失敗すると、
  AND-OR リスト末尾の失敗は set -e を発火させ**その行でスクリプトが即終了**。負けた側は
  new-window にも display-popup にも到達せず、メニュー選択が無言で消える
- **修正**: `tmux new-session -Ad -s "$sess"` (attach-or-create、既存なら成功) に置換するか、
  `|| tmux new-session -d -s "$sess" 2>/dev/null || tmux has-session -t "$sess"` で再確認する

### 7. resurrect wrapper が save 失敗 (rc≠0) 時に pane_contents バックアップを復元せず削除する

- **場所**: `scripts/tmux_resurrect_save.sh` (Fix B2 の後始末 — `rm -f "$tt_archive_bak"` が rc 不問)
- **検証**: 📖 コード確定 (Fix B ブロックの `[ "$tt_rc" -eq 0 ]` ガードと無条件 rm を確認。
  「archive 半書きで kill」の実行再現は未実施)
- **機序**: upstream save_all は layout dump → `ln -fs` (last 前進) → pane_contents archive 生成
  (`gzip >` で共有 archive を truncate 上書き) の順。archive 生成中に save.sh が死ぬと
  (サーバ終了との重なり等)、last は完成 layout を指すが共有 pane_contents.tar.gz は壊れた gzip になる。
  このとき wrapper は rc≠0 なので Fix B の復元ブロックを丸ごと skip し、最後の掃除で
  **無傷のバックアップを無条件に rm** する。rc≠0 はまさに「archive が中途半端な可能性が最も高い」
  ケースなのに復旧データを捨てている。次の保存成功までにクラッシュ→復元が起こると、
  全 pane のスクロールバック復元が tar 展開失敗で silent に失われる (次の保存成功で自己治癒はする)
- **修正方向**: rc≠0 かつバックアップ存在時は、archive がバックアップと異なる (mtime/size) なら
  バックアップを `mv` で書き戻してから return する

### 8. window-name: `\nvim` (alias バイパス) でアイコンが外れタイトルにバックスラッシュが残る

- **場所**: `zshlib/_tmux_window_name.zsh` (`_tmux_extract_command` のトークン無加工採用)
- **検証**: ✅ プローブ実証 (`\nvim file` → `_default アイコン + " \nvim"` を od で確認)
- **機序**: `${(z)...}` はバックスラッシュをトークン内に残すため、`\nvim` は YAML の `nvim` キーに
  ヒットせず `_default` フォールバックになる。表示劣化のみ (機能停止なし)
- **修正**: cmd 先頭の `\` を 1 個剥がしてから YAML 照合する (`cmd=${cmd#\\}`)

### 9. window-name テストの Test 5 が per-entry のアイコン回帰を検出できない

- **場所**: `tests/zshrc/tmux-window-name/test_tmux_window_name.sh` (Test 5 の assert_contains)
- **検証**: ✅ プローブ実証 (YAML の nvim 行を潰しても Test 5 相当の assert が PASS することを確認)
- **機序**: `assert_contains "nvim" "$result"` は、YAML ヒット時も `_default フォールバック +
  コマンド名` 時も真になる (フォールバック文字列にもコマンド名が含まれるため)。個別エントリの
  タイプミス/消失を検出できない。YAML 全滅級の回帰は Test 14 が拾うことも確認済み (被害は限定的)
- **修正**: Test 5 を `assert_equals " nvim"` (YAML 定義の正確な文字列) に変える

### 10. tests/tmux/test_tmux.sh の partial false-pass (既知・別タスク化済み)

- **場所**: `tests/tmux/test_tmux.sh` (`assert_no_style_leak "L" "$(...)"` / source-file の `|| true`)
- **検証**: ✅ プローブ実証 (本 issue とは別の監査で確定済み。修正はタスクチップ
  task_aaac2ca8「test_tmux.sh の partial false-pass を修正」として起票済み)
- **機序 (要旨)**: command substitution を**関数引数に埋め込む**と set -e が効かず、display-message
  自体が失敗した場合に空文字が assert に渡って無音 PASS になる。狙った回帰 (スタイルリテラル漏れ)
  自体は現在も検出できるため partial。詳細と修正方向はタスクチップ側に記載

---

## 棄却した指摘 (再監査での再生成防止のため記録)

- **popup close 判定に claude-fork が無い**: `C-t t` の close 分岐が scratch|launcher のみで
  レジストリ (scripts/lib/tmux_popup_sessions.sh) の 3 つ目 claude-fork を含まない、という指摘。
  機序は正しいが、**fork popup は現在意図的に無効** (_tmux.conf:493-494 のコメント: A/B 観測のため)
  なので現状は発火不能 → 棄却。**fork popup を復活させるときは close 分岐への claude-fork 追加が
  必須** (この条件付きで将来の再評価対象)

## 監査メタ

- 発見 12 件 → 敵対的検証 (反証デフォルト) → 確定 10 / 棄却 2。
  検証エージェント 3 件が session limit で死んだ分 (#3, #6, #7 の元指摘) は main agent が
  コード直読で再検証した (#6, #7 は実行プローブ未実施のため上記のとおり 📖 表記)
- 既修正の再指摘は除外済み: e7bb0d7 (GC が adopt 済み live hold を誤 kill する問題 —
  @tt-adopted による同一 boot 内保護) / fffeb76 (extract-popup の literal 化)
