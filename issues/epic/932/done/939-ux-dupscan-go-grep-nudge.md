# 939 (ux): Go指定のgrepに対応したdupscanコマンドを案内する

起票日: 2026-10-27

## 問題と範囲

[938](../938-ux-dupscan-agent-friendly.md)の案1を、独立して検証・完了できる課題へ切り出した。Swift/Go混在repoで `rg -t go BuildDefinitions .` を打つと、従来のhookはSwift専用の `dupscan def/refs` を案内していた。Goのみを追跡するrepoでは案内が出なかった。

対象は既存Claude PreToolUseの `_claude/hooks/suggest-dupscan-for-symbol-grep.sh`。Goを明示した検索に、Goで使えるindex/callersを案内する。938の案2〜5、928のregistry/capabilities/no-build、Codex固有のhook対応は本issueで実装しない。

## 実装

- Bashは `rg -t go` / `-tgo` / `-t=go` / `--type go` / `--type=go`、`--include=*.go` / `-g '*.go'`、実在するGo fileや `git grep ... -- '*.go'` を手掛かりにする。短いflagの安全に判定できるbundle（`-nt go` / `-ntgo` / `-nt=go`）も扱い、bundled `i` は通知を抑える。すべてのshell構文を解釈する機能ではなく、既存のshlex近似の範囲に留める。
- Grep toolはtype=go、Go glob、実在するGo fileのpathを読む。正規表現・大小文字無視・ログ等の非source・曖昧なGo/Swift selectorは控える。除外selectorを正のGo対象とみなさない。
- 言語/名前の判定後、cwdのrepoが該当言語のsourceを追跡していることをgit ls-filesで確認する。dirの内容を走査して言語を推測しない。Swift向けの既存判定/案内本文は維持する。
- Goの案内は `dupscan index -name <末尾名>` / `dupscan callers '<末尾名>'`。Type.memberでもTypeを解決したとは言わず末尾名を使う。名前による近似、Goでdef/refs/contextが非対応であることを明記する。
- hookはdupscan/go/buildを起動せず、additionalContextだけを返す。grepを拒否しない。未実装registryを呼ばず、対応する2コマンドの短い文言だけを既存hookへ足した。928でregistryを導入するときに共用へ寄せる。
- Go通知のstamp prefixを別namespaceにし、既存Swiftのstampを維持する。同一言語は既定900秒に1回。Go通知がSwiftの間隔を消費しない一方、言語ごとに最大1件ずつとなる追加contextは発生しうる。

## 受け入れ条件・検証結果

- [x] 明示的なGo検索はGoのindex/callersだけを案内し、def/refs/contextの実行を勧めない。
- [x] Swift既存43ケースを維持し、Go指定/混在/否定/通知間隔/build非起動の追加41ケースを含め84 passed / 0 failed。
- [x] 修正前hookとSwift向け14条件のrc/stdout/stderrをbyte比較し一致。
- [x] same-sessionのGo 2回目は抑制、続くSwift通知は従来どおり出る。Goだけのrepoでも案内する。
- [x] fake dupscan/goをPATH先頭に置いたspyが起動されないことを検証。案内は出るがcompiler/解析は走らない。
- [x] 実dupscanでGoのBuildDefinitionsをindex/callersから引けることを確認。Goの本体code変更は無いため重複gateの対象Go変更も無い。
- [x] shell構文/shellcheckと対象hook84ケースを確認。広い適用検査の環境依存失敗は下に分けて記録した。
- [x] 反証レビューを通し、問題のある短縮option表記を回帰へ固定する。

## 反証レビュー・計測

読取専用Codex subagentがP2を1系統指摘した。`-ntgo/-nitgo`、`-t=go/-nt=go`、`-g=!*.go`を正しく解釈できず、GoをSwiftへ誤案内したり、Go除外をGo対象とみなした。短いtype optionのbundle/任意の=を扱い、短いglob optionの=だけを外して正負を判定するよう訂正した。mixed repoに回帰を追加し、実rgとhookの再レビューで解消済み。ほかに修正必須の指摘なし。

Swift案内の待ちを、同じ混在fixture・warm-up後5回・前後交互順で補助観測した。現在の中央値はbefore 84.61 ms / after 85.09 ms。CPU/OS負荷を制御しておらず、性能改善や退行を断定しない。目的はCLIへの誤案内を避けることで、hookそのものを高速化する変更ではない。

一時原本は `/private/tmp/939-all-tests.log`、`939-required-checks.log`、`939-swift-compat.json`、`939-hook-latency.json`、`939-compat.py`。一時資料の存続は保証せず、結果・全数勘定と限界は本issueへ転記した。

## 改善評価・残る限界

混在repoのGo指定で非対応だった2コマンドへの案内を0にし、Go-only repoにも対応する探索の入口を追加した。既存Swiftの本文/間隔は変えていない。追加コストはselector判定と、言語別通知による最大1言語分の案内。誤った新規メソッド追加率・再利用率・実context tokenの改善は未測定で、928の評価で継続する。

921の旧集計はSwift repo内のdupscan全呼出しを数えるため、Go向け利用が増えたときにSwift効果と混同しない。元のraw集計/測定時期は維持しつつ、再測定ではGo指定の検索/明示Go scopeのdupscanを別集計し、言語不明をunknownとして残す。938の「Go指定なら集計へ影響しない」という記述はこの点を訂正する。

## 進捗

- 2026-10-27: `issues: reserve Go-aware dupscan grep guidance`で番号予約し、938案1を分離。`fix(hooks): guide explicit Go symbol searches to supported dupscan commands`で実装した。registry/残るUX案を完了扱いにせず、939の範囲を独立して検証した。

- 2026-10-27: 最終scopeのBash3.2/5を直接比較し20条件でrc/stdout/stderrが一致。Go stampも別prefixにし、session IDが末尾-goの別Swift sessionと衝突しない2回帰を追加した。
- 2026-10-27: `make test-changed`のsyntax/shellcheck/zsh/lintとClaude回帰を実行。全体コマンドは2件失敗したため総合passとはしない。tmux pane-state bellはTERM=dumbのハーネス失敗で、TERM=xterm-256colorでの単独再実行は成功。dangling_symlinksは `~/.config/nvim/coc-settings.json -> ~/dotfiles/_coc-settings.json` の既存リンク切れで、修正前5b6d74a5の同テストでも再現した。ユーザー設定は変更していない。claude_links_completeの1件はworktreeでの既定skip。対象hookとshellcheckは最新sourceでも成功した。

補助latencyの全値（秒）はbefore 0.082895 / 0.083423 / 0.085620 / 0.085401 / 0.084614、after 0.085525 / 0.085093 / 0.085511 / 0.084950 / 0.084610。

- 2026-10-27: 追加回帰を修正前hookへ向けた一時コピーでは55 passed / 29 failed、修正後は84 passed / 0 failedとなり、Go案内の欠落/誤案内を検出できた。最終の文書反証レビューでP3を1件修正し、938の「今の判定」を実装前の履歴へ明示した。

- 2026-10-27: `docs(dupscan): complete Go grep guidance and track remaining UX ideas`でskill/完了記録を保存し、939の実装範囲をdoneへ移す。938の残りと928のregistry/効果測定は継続する。

- 2026-10-27: issueの適用整合検査、done移動後の相対リンク検査、diff検査が成功。対象hookの84回帰・shellcheck・Swift互換14条件・Bash互換20条件は成功。全体test-changedの環境依存失敗は上記のとおり残し、総合passとは報告しない。
