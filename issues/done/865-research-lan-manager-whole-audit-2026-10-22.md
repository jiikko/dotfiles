# 865 (research): lan-manager 全体監査 (2026-10-22) の全数勘定と、攻めたが見つからなかった範囲

起票日: 2026-10-22

## 概要

`/audit` の全 22 タイプを直接実行で `src/mac-lan-manager` 全体に当てた (ユーザー指示 2026-10-22)。調査体は 1 体ずつ直列に 4 波で回した
(security・broken-code・error-handling・false-green / resource-leaks・concurrency・performance・dead-code・dependency / 設計 7 タイプ /
test-cleanup・test-helpers・ux・ci・lint-from-done・general)。どの体も読み取りのみで、検証用のテストは `go test -overlay`、変異は `mutate-verify` の使い捨て worktree で当てた。
P2 の発見と「今すでに食い違っている」の主張は、main agent がコードで照合してから採った。

監査の途中に 85f306a6 (速度測定の進捗モーダル) が入り、第 3 波は入った後の木で読み直した。847 (reach) は別のセッションが実装中で、その範囲の発見は 847 側の判断に回すと各 issue に書いた。

## 母集合

- 非テストの .go: 36 ファイル・9,362 行 (11 package)。テスト: `func Test*` 338 本
- 基準の `go test -count=1 ./...` は rc=1 (setup の `TestRemoteCommandRunsDetached` だけ。flaky で 864 の A1)。`-race` の全体は緑
- 既出の判定に使った issue: `grep -rl 'lan-manager\|lanmgr' issues/` の open / done

## 全数勘定 (発見の行き先)

| 行き先 | 件数 | 中身 |
|---|---|---|
| 859 (P2) | 2 | speedtest が途中で切れた測定を成功にする (実行で確認) / runSpeed の止める合図 |
| 860 (P2) | 2 | `s` の HostKeyAlias が未検査 / dump の文字出力が無害化されない |
| 861 (P2) | 1 | ルーターの無い Wi-Fi を有線と数える (rows) |
| 862 (P3) | 8 (+ 却下 1) | ping の lan_mismatch の rc のテスト・部分失敗・wiredCheck のテストの偽物・unescape・台帳の 2 回の lock・unadvertise・header の待ちの文・ObservePeer の IP の段。InstallScript の bootstrap は 825 で見送り済みなので却下 |
| 863 | 15 | 設計系 (P1〜P4・P6〜P12・U1〜U3。P5 は 861 へ) |
| 864 | 8 (+ 記録 1) | flaky・CI のテストの所要・テストの穴・helper・lint |

タイプ別の 0 件:

- dead-code: `staticcheck -checks U1000` は `-tests=false` の有無とも 0 件 (canary の module で U1000 が出ることを確かめた)。`deadcode` (go1.25.4 で build) は test 専用の
  `internal/testsandbox.New/Guard` だけ。ただし `-whylive` が生きているメソッドにも "not found in program" を返すので信用していない。
  代わりに mac-lan-manager と glogx の非テストの公開関数 121 個の参照を数え、0 件は `Unwrap` (error)・`Init` (tea.Model)・`Guard` (test 専用) だけ。公開関数からしか呼ばれない死んだ連鎖は除外できていない
- dependency: `GOFLAGS=-mod=readonly go mod tidy -diff` は rc=0・差分なし。bubbletea / lipgloss / x/ansi / x/sys / x/sync などの版は glogx・tuikit と一致し、replace 先も一致
- responsibility / design: 863 の「0 件」
- ux: 下の掃引で溢れ 0 件

## 攻めたが見つからなかった範囲 (次の監査の起点)

- 相手へ送る sh (StaticIP の Script / RemoteCommand / ConfirmCommand、Install / Remove / StatusScript、KeyArgv): 埋め込む値は全部 Validate を通ってから Quote され、引用を破る経路は無い。宛先の前の `--`、ユーザー名の先頭の `-` の拒否、ping の引数の IPv4 への正規化も効いている
- デッドマンスイッチ: 確認なし・シグナル・lock の取り合い・期限のずれ。確認を待たずに台帳へ IP が書かれる経路は無く、期限の後の確認は ConfirmCommand が非 0
- 台帳: 新しい版の台帳・厳しい読み・flock (O_CLOEXEC で子へ継承しない)・O_NOFOLLOW・不正な欄の捨て方
- `ssh -G` が known_hosts の置き場所を絶対パスに展開して返すこと (実測)
- プロセス: `discover.Scanner` の Close / wg / sem / 再収集のループ、ExecStarter の Wait / Kill、speedtest の goroutine の終わりと取り消しの Wait、CLI の dumpPeer / runPing / runSpeed / settle、全 exec に subproc 経由の WaitDelay。テスト後の PPID=1 の dns-sd は利用者の LaunchAgent の広告 (5 日前) だけ
- CI の C1: replace 先 5 module とその推移的な依存 (tuikit → termsafe) は全部 paths に入る。テストが module の外を読むものは無い。`t.Skip` は 0 件。mac-lan-manager を変えると glogx と pro-con の workflow も走る
- ux の掃引 (`go test -overlay`): 15 状態 (list / ? / c / / / e / , / 帯 / m / u / space / 未管理タブ / 未管理の space / enter) × 幅 1〜140 × 高さ 1〜45 (奇数)、
  速度の板・測定中のモーダル・疎通中・初期設定 × 幅 1〜140 × 高さ 12 点で溢れ 0 件。各状態が実際に開いたことは ov / sc の値で確かめた。幅の関数は `lipgloss.Width` と `termwidth.Of` が混在するが、どちらも元は ansi.StringWidth で食い違う入力は見つからない
- 設計: 循環依存なし。main と glogx の exec / waitChange の重複は意図的とコメントにある。IP は `discover.ParseAddr` と `ledger.ValidLAN` が入口で検証。lanFixed / SelfFixed / Plan の peerStatic の違いは、観測できるものの違いによる意図的なもの

## 既出として数えなかったもの

- 847 (wired_ok が名前の解決だけで true / lanFixed が台帳の値だけで「固定」と言う)、830 (1 つの I/F に 2 つの IP)
- `prefs.Save` の後勝ち: 747 の「直していないもの」で受容済み (人が 1 画面で変える)
- ssh の ControlMaster が TUI の終了後 15 秒残る・IP が変わった直後に古い master に当たる: `sshdest/sshdest.go:controlPath` のコメントで受容済み
- trimLog と FileLog のプロセス間の行の消失: コメントで受容済み

## 低い確度で残すもの (issue にしていない)

- 外部コマンドの起動が混在している (reach の ping だけ `/sbin/ping` の絶対パスで、networksetup・ifconfig・dscacheutil・ssh は PATH から引く)。実害は確認していない
- 速度の板の `Y` は「取り消し」として黙って閉じ、他の確認の板 (ovConfirm) は `confirm.IsYes` を使う。文書との食い違いではない

## 反証レビューの結果 (opus のサブエージェント・読み取りのみ、859〜862 を 1 本で)

859 と 860 の中核、861 の経路、862 の 5〜9 は反証されなかった。訂正したもの:

| issue | 指摘 | 対応 |
|---|---|---|
| 861 | done の 747・828 に記録済み (trigger 未達) / `selfLan` 側は prefs を手で書いたときしか届かない / 宛先の選び方と断りの理由にも効く (補強) | 既出と起票の理由を書いた。2 か所目の到達性を訂正。影響の 2・3 を足した |
| 860 | `*` も通る (`*.local` で全部の .local に一致) / `Dest.Validate` を丸ごと通すと有線の名前の s を壊す / ssh -G と ssh-keygen -F の実測 | 実測と `*` を足し、対応方針を `reAlias` だけに |
| 862-1 | rc=0 は README と usage の契約どおり / CLI は 🚨 を付けている | false-green の言い方をやめ、「テストで rc を固定していない」に |
| 862-2 | 方針どおりだと VPN・抜いたアダプタの相手が毎回判定できない | 除くものを方針に足した |
| 862-3 | 広告されていない名前は実機で 10 秒待つので 3 秒の上限で必ず err になる。方針どおりだと `!` が消える / テストの偽物が本物と違う | 方針を撤回し、偽物を本物に揃える項目に組み替えた |
| 862-4 | done の 825 で見送り済み・新しい実測なし | 却下 |

863〜865 は反証レビューに通していない (refactor / test / research で、issue 規約の「主要 issue だけ通す」の外。発見は main が証拠で照合済み)。

## 進捗

- [x] 4 波の監査と照合 (2026-10-22)
- [x] 859〜862 の反証レビューと訂正 (2026-10-22)
- [x] 859 で解消 (2026-10-22。speedtest の途中で切れた下り・止める合図。done へ移した)
- [x] 860 で解消 (2026-10-22。s の HostKeyAlias の名前の検査・dump の文字の出力の無害化。done へ移した)
- [x] 861 で解消 (2026-10-22。ルーターの無い Wi-Fi で見えた相手を有線と数えない・その IP の種類を「無線 (ルーターなし)」に分ける。done へ移した)
- [x] 862 で解消 (2026-10-22。P3 の 9 件 (却下 1) を全部決着。done へ移した)
- [x] 864 で解消 (2026-10-22。A1〜A8 を b9 と dotfiles-0e で分けて直した。done へ移した)
- [x] 863 は open のまま追う (2026-10-22。P8〜P12・U1〜U3 は済み。残りは trigger 待ちの P1〜P4、宛先の型の P6、847 の範囲の P7。863 の進捗が正本)。
  監査の発見は全部行き先が決まったので、この issue (全数勘定) は閉じる
