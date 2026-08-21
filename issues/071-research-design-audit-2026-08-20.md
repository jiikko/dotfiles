# 071 research: 設計監査 (2026-08-20)

起票日: 2026-08-20

`/audit` 設計バッチ (design / responsibility / duplication / polymorphism / ui-components) の
結果。forge Standard × 8 エージェント + 条件付き (refactoring-patterns /
go-architecture-designer)。統合フェーズは session limit で落ちたため main agent が集約した。

エージェントには「複雑性が実際に下がる根拠 (読む時の jump 数 / 変更時の touch 箇所 / 結合 /
重複 / 状態の局所化のどれが下がるか) を書けない提案は出すな」「issues/done で『やらない』と
決まった分割を逆転提案するな」を渡してある。実際に glogx の browseModel 分解・
diffOverlay/jobDetailOverlay 統合・toast キュー化などは既決事項として除外された。

## 最も強いシグナル: 共有観測ログの追記が 8〜9 箇所に散在し 3 系統に分裂

**5 エージェントが独立に指摘**。`~/.cache/tt-restore-trigger.log` への 1 行追記が

- 同一実装の `log_line()` として 4 箇所 (`tmux_log_kill_command.sh:46` /
  `tmux_periodic_save.sh:53` / `tmux_restore_runner.sh:33` / `tmux_verify_restore.sh:33`)
- printf インライン版が 2 箇所 (`tmux_log_session_closed.sh` / `tmux_log_conf_source.sh`)
- `TT_TRIGGER_LOG` seam を**無視して** `$HOME/.cache/...` をハードコードする版が 2 箇所
  (`tmux_resurrect_save.sh:210,216` / `tmux_reap_orphan_servers.sh:127`)
- さらに `_tmux.conf:475,478` のインライン hook (→ 070 にも記載)

に分かれている。実害が 3 つ:

1. 行書式は**読み手向けの契約** (`scripts/CLAUDE.md` が「docs/tmux-plugins.md の表が読者側の
   正本」と明記) なのに、書き手が 8 箇所に散っている
2. `TT_TRIGGER_LOG` を迂回する 2 箇所は、他 6 本のテストが確立した
   `TT_TRIGGER_LOG="$LOG"` の慣行でテストを書くと**空ファイルに assert して vacuous に緑**に
   なる (false green の温床)
3. ローテーション (`TT_TRIGGER_LOG_MAX_LINES`) が `tmux_periodic_save.sh` にしか無いため、
   共有ログの上限が「periodic_save が回っているか」に暗黙依存している

**下がる複雑性**: 書式変更・ローテーション追加の touch 箇所が 8→1、seam の尊重/迂回の
2 系統が 1 系統になり「テストから観測できない書き込み経路」が消える。集約先は既に全対象が
source 済みの `scripts/lib/tmux_resurrect_guards.sh` (自身のヘッダで「二重定義を避ける集約点」
と役割宣言済み) なので**新しい依存辺は増えない**。

## [済 7c064e6] gum confirm の `--default=false` が実装強制されていない (High 相当)

`scripts/CLAUDE.md` が「gum confirm は `--default=false` に統一する (Enter 素通しでは
実行されない)」を規約として定めているが、これを守らせる仕組みがどこにもない。

- ✓ main agent 確認: 現状 6 箇所すべて `--default=false` は付いている
  (confirm 3 本 + `_tmux.conf:407` のインライン M-c)
- ✓ main agent 確認: `tests/` に `default=false` の出現は **0 件**。テストの gum スタブは
  argv を記録しているのに、13 個の assert は 1 つも argv を検査していない
- エージェントの変異実験: 3 本から `--default=false` を全削除しても **baseline=PASS /
  mutant=PASS**。対して `&&` → `;` (fail-safe 短絡の破壊) は FAIL になる
  = 同じ CLAUDE.md の隣り合う 2 行のうち、片方だけが実装強制されている非対称

**推奨**: (1) 既存スタブに `assert_called "gum confirm --default=false"` を 1 行足す
(新しい仕組みは不要)、(2) `discover_shell_scripts.sh` と同じ発見方式の grep ゲートで
`scripts/*.sh` と `_tmux.conf` の `gum confirm` を列挙し `--default=false` を伴わないものを
fail にする (発見 0 件も fail)。これで `_tmux.conf:407` のインライン confirm も対象に入る。
(3) M-c のインライン confirm を `scripts/` へ寄せると、confirm ファミリーの fail-safe 契約と
テスト資産 (nogum スタブ) を再利用できる

**対応 (7c064e6)**: (1)(2) を実装。個別 assert を 2 本足し (引数順に依存しない形)、
`tests/tmux/test_confirm_default_gate.sh` を新設して `make test-lint` に組み込んだ。
ゲートは呼び出し単位で検査し、検査対象は実行されるコード全体を発見式に列挙する
(`scripts/lib` / `bin` / `zshlib` / `_claude/hooks` も含む)。敵対的レビューで 8 件の
素通り経路が出たため全部塞いだ (同一行のコメントに文字列だけ置く / 1 行に 2 つ目の confirm /
検査対象外ディレクトリへ移す / `"$GUM" confirm` と行継続分割 / 偽 grep で false green ほか)。
**(3) の M-c 移設は未着手** (ゲートが `_tmux.conf` のインライン呼び出しも検査対象に含むため
緊急度が下がった。trigger: M-c の confirm を次に触るとき)

## 状態 → 表示の写像が 3〜5 箇所に独立実装

`@claude_state` (working / input / seen / idle) から**色・記号・ソート順**への対応表が
`_tmux.conf` の window list format / `scripts/tmux_agent_panel.sh:295-311` /
`scripts/tmux_agent_jump.sh` に独立実装されている (4 値の enum に対する典型的な
polymorphism 候補)。状態を 1 つ増やす / 色を変えるときの touch 箇所が 3〜5。

同種: agent panel の quiet gate (`@agent_panel_busy` からの経過秒) が `bin/tmux-toast:65` と
`scripts/tmux_resurrect_debounced_save.sh` で**形の違う二重定義**になっている。

## 常駐 lock の取得手順が 3 スクリプトに verbatim 複製

`tmux_periodic_save.sh:58-80` / `tmux_server_watchdog.sh` / もう 1 本で、stale 掃除ループ +
`tt_lock_*` 呼び出しの並びがほぼ逐語同一。068 で露呈した「owner 書式の出典が複数ある」問題と
同じ根 (lock の read/write/掃除を guards.sh に寄せると両方閉じる)。

## glogx (Go)

- **pager 3 面の描画パイプラインが独立に手書き** — `diff_overlay.go:95-120` /
  `job_detail_overlay.go:82-100` / `statusView.pagerBox`。3 者すべてが同順・同書式で
  「loading→スピナー行 / 空→(〜はありません) / それ以外→clamp して本文」を組む。
  しかも `statusView` だけ描画時に `clampScrollOffset` を呼ぶ差分がある (correctness 側の
  ずれ)。さらに `job_detail_overlay` だけスクロールキー語彙を共有関数 `pagerScrollKey`
  (`scroll_glide.go`) に委譲せず手書きしている — 共有関数の doc が「pager 面共通」と
  宣言しているのに 1 面だけ外れている形
- **全画面 viewer 2 枚が開閉スライドの状態機械を独立に 2 コピー持つ** —
  `issues_view.go:260` 付近と対の viewer
- **グローバル chrome の合成が 2 箇所に逐語コピー** — `tui.go:2853` (`finishViewerWindow`) と
  `tui.go:2938-2953` (`viewLines` 末尾)。doc コメントが「ビュー共通」と主張している側が
  片方だけ
- **`actionModal` が相互排他な UI 状態を 7 本の bool で持つ** — `action_modal.go:18`
  (pushConfirm / pushing / pullConfirm / pulling / …)。排他不変条件が型で表現されていない
- **y/N 確認の「実行キー」述語が 3 箇所で独立実装** — `issues_view.go:1834` /
  `action_modal.go:78` 他。**大文字 `Y` の扱いが箇所ごとに違う** (correctness 寄り)
- **`tui.go:1296`** の status viewer routing が、viewer がキー待ちモーダルを持っている間も
  `p` / `b` / `U` を拾う

## repo 全体の方針との不整合

- **`Makefile:241` の `GO_PROJECT_DIRS` が手動列挙** — この repo は「登録なしで自動的に対象に
  なる」を全域で徹底している (`SHELLCHECK_FILES` は `discover_shell_scripts.sh`、
  テストは `test-dir` の自動発見)。Go プロジェクトだけ手動なので、新しい `src/*` が
  黙って lint / test の外に落ちる
- **`bin/ci-log:43`** の `grep -q . || echo` が `set -e` 下で意図と違う分岐になる

## Swift / Terminal プロファイル

- **色キーの一覧が 2 言語に二重定義** — `scripts/lib/terminal_profile_colors.swift:29` の
  4 キー配列と `scripts/terminal_profile_restore.sh:42-49` の case。5 つ目を足すと
  shell 側の `*) continue` が**黙って捨てる** (無音の縮退)。この 2 ファイルにテストは 1 本も
  無い。対応は Swift 側が最初から AppleScript のプロパティ名で出力し、shell の case を消す
- **旧 NSArchiver フォールバックが捕捉不能な ObjC 例外で abort する** —
  `terminal_profile_colors.swift:26`。エージェントが Swift 6.2.4 / SDK 26.2 で実測再現
  (`streamtyped` の truncate 済み blob → SIGABRT)。その結果 :30-35 の診断メッセージ経路が
  デッドパスになっている。フォールバック自体が救う対象 (repo 外の旧ファイル) が
  ほぼ無いので削除が第一候補

---

## 反証・対応の結果 (2026-08-21)

⚠️ **この節が唯一の durable な記録**。裏取り/反証のレポートは `./tmp/` に出したが `tmp/` は
gitignore 対象なので残らない。以降の audit はこの節を先に読むこと (却下済みの指摘を再生成しない)。
「反証できなかった」は「正しいと証明された」ではない。

### 対応済み

| 指摘 | 対応 |
|---|---|
| gum confirm の `--default=false` が実装強制されていない (High 相当) | 並行セッションが `7c064e6` で `tests/tmux/test_confirm_default_gate.sh` を新設。**その新ゲート自身に穴**があり (`grep` rc=2 を無マッチと同一視して読めない違反ファイルを黙って除外) `52f62ec` で 3 分岐へ修正 |
| status viewer routing が pager/破棄確認中もキーを横取り (`tui.go:1296`) | `a9f3fa5`。**全画面 diff の `b` → `Enter` で実 `git push` が走る** / X の破棄確認中に `p`→`y`→`y` で作業ツリー破棄、の 2 経路を実測。`statusView.ownsKeys()` で横取りだけをガード |
| `Makefile` の `GO_PROJECT_DIRS` 手動列挙 → 方針との不整合 | 未着手 (下記の trigger 待ち) |
| `test_changed.sh:142-143` の写像が shell 系を一部にしか写さない | `7f83abb`。登録表を作らず**参照からの発見** (basename を `tests/*/` から grep) へ。`tmux_agent_panel.sh → tests/tmux` 等が届くようになった |
| pager 3 面の重複のうち **offset の書き戻し非対称** | `512b490`。rows 増加後に上スクロールが (rows 差) 打鍵死ぬ (diff 33 / job 詳細 11 を実測)。描画で論理 offset を収束させる形へ。**論理 offset を直接見る assert** も追加 (既存テストは title しか見ておらず退行を捕まえなかった) |
| `actionModal` の 7 bool 排他崩れ | **未着手**。issue 074 (claude/codex update の並列化) が同じ `action_modal.go` を map 化する予定で、そちらで設計ごと解消できる可能性がある。単独で応急処置 (`tui.go:1035` を `active()` に広げる) を当てると 074 と衝突する |
| 共有観測ログの 8〜9 箇所分裂 | **未着手**。ただし根拠の 1 本 (`071-triglog-seam-bypass` = seam 迂回が false green の温床) は反証で崩れた (下記)。集約そのものの価値は残るが優先度は下がる |

### 反証で崩れた (却下) — 再提案しないこと

| 指摘 | 崩れた理由 (要点) |
|---|---|
| 観測ログの seam 迂回が「vacuous に緑」の温床 (`071-triglog-seam-bypass`) | seam 不在・`$HOME` 直書きは事実だが、repo の assert は全て positive grep なので `TT_TRIGGER_LOG` で書くと**即 false red**。誤緑になるのは否定 assert のときだけで、それは変異検証規約が潰す形。census も不完全 (`_tmux.conf:475,478` を落としており「3 箇所」でない) |
| `check_syntax.zsh:53` が警告付きロードを pass にする | conf 警告検査の責務は `tests/tmux/test_tmux.sh:100-114` に**意図的に置かれており**、`make test` と CI の rest 腕で実際に走る。主張の 3 クラス全部で suite が赤になることを実測。`make test-syntax` 単体が緑なのは分業どおり |
| `job_detail_overlay` が `pagerScrollKey` に委譲していない | 骨だった「共有関数の doc が『pager 面共通』と宣言」の引用が**現物に存在しない** (grep 0 件) ため破られる契約が無い。ドリフト実績 0。委譲すると既知 P1 (tick が返らず表示が固まる) を再生産する |
| y/N の「実行キー」述語の 3 箇所独立 (大文字 Y の扱い差) | 厳格側の `Y` は「安全側で閉じる」、寛容側は「意図どおり実行」で失敗シナリオが作れない。厳格側は doc + テストで pin 済みの契約で、**揃える方が不変条件を壊す** |
| `_zshrc:696-733` の適用漏れ通知が結果ファイルを共有 | 通知は消えず、最初に prompt へ到達したシェルが 1 回表示し、state 非更新により後続シェルが**再通知**する (実測)。hook 居残りは宣言済み設計の縮退形で拾い手として機能 = **実害の向きが逆** |
| 色キーの 2 言語二重定義 | 070 側の同項目参照 (sdef が単一ソース。5 つ目の arm は書けない) |

### 未着手 (trigger 待ち)

`071-lock-acquire-verbatim-dup` / `071-chrome-composition-dup` / `071-two-slide-state-machines` /
`071-pager-empty-branch-divergence` / `071-triglog-writer-count` / `071-triglog-rotation` /
`071-go-project-dirs` / `071-nsunarchiver-uncatchable-abort` / `@claude_state` 写像の集約 /
observation log の集約。いずれも裏取り側が「発火条件を示せない」または反証未実施。

### 指摘の文言の訂正 (offset の項)

- trigger は「**幅**を広げる」ではなく **高さ (rows) の増加**。幅だけ 40→200 では dead k = 0
  (幅由来なのは既修正の issues 本文 pager だけ)
- 「32 打鍵」は固定値ではなく `rows_new - rows_old`

