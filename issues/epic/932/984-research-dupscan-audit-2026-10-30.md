# 984 (research): src/dupscan の 22 観点の監査 (直接実行) — 全数勘定と、直さないと決めたもの

起票日: 2026-10-30

## 概要

`/audit` で src/dupscan に 22 観点を直接実行 (サブエージェントに判定させず、main が読んで判定) で回した。HEAD `a335a9c42`。
直す項目は [981](981-bug-dupscan-gate-silent-on-generated-go-files.md)・[982](982-cleanup-dupscan-unused-text-writers.md) の 2 本。[983](done/983-bug-dupscan-paths-levels-l1m.md) は反証レビューで 973 の決定と分かり取り下げた。ここには観点ごとの攻め方と、報告しなかったものの理由を残す (次の監査が同じものを出さないため)。

## 走査した母集合

- production の Go: 54 ファイル・13,150 行 (`find src/dupscan -name '*.go' -not -name '*_test.go' -not -path '*/third_party/*'`)。テストを含めると 127 ファイル・26,671 行
- 全部を読んだのは main.go・gate_cmd.go・internal/gate・internal/group・internal/norm/norm.go・internal/exclude・internal/repo・internal/report/gate.go・internal/lang/lang.go・internal/langs・internal/tree・internal/lang/golang/golang.go。
  internal/lang/swift/swift.go (2110 行) は Parse と読み直し (`reparseKnownMisparses`) だけを読み、残りは関数の一覧と変更履歴で見た。**sweep_cmd.go・near / search / index / context / evidence の各コマンド・internal/lang/golang/conv.go は精読していない** (次の監査の起点)
- 機械の検査: `go vet` rc=0 / `go test -count=1 ./...` 全 package ok / `deadcode` と `deadcode -test` (→ 982) / `bin/dupscan sweep src/dupscan` (単位 667) / `go list -m -u all`
- 並行セッション (dotfiles-38) が doctor・agent-guide を消す作業中だったので、その 2 コマンドは対象から外した

## 観点ごとの結果

| 観点 | 結果 | 攻めた所・報告しなかった理由 |
|---|---|---|
| design | 0 | 作業ツリーと commit を同じ形で読む `repo.Source`、gate の出どころの当て方 (`gate.origins`)、除外の釘の効き方 (`exclude.Set.known`) を読んだ。設計の注記と実装の食い違いは見つからなかった |
| responsibility | 0 (見送り 1) | swift.go は変更の理由が 3 つある (文法の読み違えの回避 954・958 / def・refs 921・922 / 単位の取り出し)。読み直しの関数群は自己完結しているが、同じ package 内のファイル分けは複雑性を移すだけなので報告しない。**再評価の trigger**: 文法の版を上げて読み直しの規則が要らなくなったとき、または規則を 1 つ足すとき |
| duplication | 1 (→ 982) | sweep の節 1 の `writeFile` (gate_cmd_test.go) と `write` (internal/repo/repo_test.go) は別 package の 12 行で、共有には新しいテスト用 package が要るので見送り。節 2 の 8 本の wrapper は既に allow-group 済み。`gate.matchLoose` の 2 組 (L1m) は照合の鍵の並びの表で意図どおり。`EvidenceText` と `EvidenceTextGrouped` の写しは 982 |
| polymorphism | 0 | 言語は `lang.Adapter` の表、コマンドは `commandSpecs` の表で振り分けている。種別の switch の散在は無い |
| leaky-abstraction | 0 | `lang.Options` の Defs / Refs を Go の adapter は集めないが、`Adapter.Symbols=false` で宣言し `symbolSource` が見て案内を出している (L4 の嘘ではない) |
| encapsulation | 0 | `exclude.Rule` の非公開の欄 (`levels` / `level`) は `check` が作るが、`groupCommentRule` の人数が合わない経路だけ手で `level` を入れる。書く場所は 1 つで、崩す経路は見つからなかった |
| ui-components | 0 | CLI の text の出力。ファイル名の見出しは `fileHeading` に寄っている |
| ux | 0 (既知 1) | `sweep src/dupscan` (module の dir から) では、正しい allow-group (8 本の wrapper) が L1 に限られて効かず、節 2 に出る。973 で決めた取捨 (起点の外に同じ言語があると L1 にだけ効かせる) どおりで、出力にも理由が出る |
| security | 0 | 子プロセスは全部 argv (shell を通さない)。git には `--` と `:(top)` を付ける。`read_argv` は JSON の配列、rg の案内は `shellQuote` で引用。`-record` は渡されたパスに書く (意図どおり) |
| resource-leaks | 0 | `Commit.ReadAll` は失敗の経路で git を止めてから待つ。tree-sitter の木・parser は閉じる。正規化の作業領域は大きく育ったものを pool に戻さない |
| broken-code | 0 | 大文字小文字の違う起点 (`SRC/DUPSCAN`) で `rev-parse --show-prefix` が正規化することを実測した (base と head の範囲がずれない) |
| dead-code | 1 (→ 982) | |
| error-handling | 0 | 捨てている戻り値 (38 か所。`grep -rn --include='*.go' -E '(^|[^a-zA-Z])_ = |, _ :?= |_, _ = '` から `_test.go` と third_party を除いて数えた) を全部見た。`strings.Cut` / sha256 への書き込み / 理由コメントのある base の許可の誤り (`gate_cmd.go`) だけ。記録を最終の rc より前に書く件は 976 |
| false-green | 1 (→ 981) | CI と make が `-strict-analysis` を付けないので、読めない構文の変わった単位は rc=0 で通るが、1 行目に数が出る (929・959 で決めた形。報告しない) |
| performance | 0 | dotfiles の src (Go 1308 ファイル) で report 0.17〜0.31 秒、gate 0.37〜0.38 秒 (最大 RSS 約 370 MB。各 2 回、`/usr/bin/time -l`、2026-10-30)。gate は base と head を両方全部読むが、この規模では直す価値が無い。CI の導入費用は 977 |
| concurrency | 0 | `parseAll` の worker は別々の添字に書く。`AppendRecord` は O_APPEND の 1 回の write。`make test` は `-race` |
| test-cleanup | 0 (浅い) | テストの変異は当てていない (次の監査で深掘りするなら、gate の `origins` の段ごとの変異) |
| test-helpers | 0 | duplication の行と同じ (別 package の 12 行) |
| dependency | 0 (観測 1) | 文法 `alex-pinkus/tree-sitter-swift` に新しい pseudo-version (2026-10-13) がある (使っているのは 2026-10-04)。上げると 958 の読み直しが要らなくなるかは**未確認**。`govulncheck` は手元に無く**回していない** |
| ci | 0 (観測 3) | 監査の HEAD (`a335a9c42`) では `test-unused-excluding-tests` が CI で赤だった (`relevantParseErrors`。監査中に別のセッションが `2f146e3e8` で直した。私は見落としていた)。src_dupscan の run 40897135377 (2026-10-29 15:00 の push) が runner 待ち (`queued`) のまま残っていた (runner の label の問題で dupscan の構成ではない)。`src/dupscan/Makefile` が include する `make/serial-run.mk` と、gate が通る `bin/dupscan`・`bin/lib/go_autobuild.zsh` は各 workflow の paths に無いが、CI では serial-run を包まない・wrapper の変更は写しを足さないので報告しない |
| lint-from-done | 0 (→ 982 の 3) | 公開名の未使用を `unused` が数えない形を、lint の段に `deadcode -test` を足すかの検討として 982 に書いた |
| general | 0 | 上の各行 |

全数: 発見 2 (981 bug・982 cleanup)、取り下げ 1 (983)、見送り・既知・観測 7 (responsibility 1・ux 1・false-green の strict 1・dependency 1・ci 3)。

## 反証レビュー

- 2026-10-30、opus の読み取りのみのサブエージェント 1 本に「誤りだという前提で反証」させた。P1 3 件・P2 2 件・P3 2 件
- 採った: 983 は 973 で決めた仕様 (README に記載・`elements` の件は 973 の P3 に記録済み) → 取り下げ / 982 の `relevantParseErrors` は master で消済み → 表を直した / CI の unused の赤の見落とし → ci の行に書いた / 38 か所の数え方が書かれていない → 正規表現を書いた / `CallersText` を消すときの doc の移し先 → 982 に書いた / worktree が master より遅れていた → rebase してから書き直した
- 採らなかった: 「`evidence` / `context` の text の出力が `Fprintf` の誤りを捨て、stdout が閉じていても rc=0」。閉じた stdout で `evidence` / `context` / `callers` / `index` / `gate` を打つと全部 rc=2 だった (2026-10-30。先に出す行の書き込みで誤りを拾う)。途中で切れるパイプでは Go のプロセスは stdout への書き込みで SIGPIPE で終わる。rc=0 になる入力を示せないので記録だけにする。**再提起の条件**: stdout の途中の書き込みだけが失敗して rc=0 になる実行を示す
