# 938 (ux): dupscan を LLM agent に優しくする — コンテキスト消費と発見性のアイデア

起票日: 2026-10-26

## 概要

agent が dupscan に気づき、少ない出力で正しい次の一手へ進めるようにするアイデアの置き場。各案に、既存 issue のどこに馴染むか・
どこと食い違うかを書く。**既存 issue が担当している論点は書き写さず、そちらを指す**。案1は939へ切り出して実装済み。案2〜4はcontext/evidenceのtextへ実装し、案5は任意のsummaryとして比較して既定短縮を見送った。出力bytesの比較は下記。開発全体の効果の評価は
[928](../928-feat-dupscan-agent-onboarding.md) の評価手順 (総 context 量と不要な重複追加を同時に見る) に載せる。

## 前提にする既存の決定

- [926](../926-feat-dupscan-query-diagnostics.md): 既存コマンドの `-format text|json|xcode` の正常な出力と終了コードを維持する (golden での固定は 926 の未完了の受け入れ条件で、
  `src/dupscan` にはまだ golden のテストが無い)。machine 向けの新しい契約は明示した `-format json-v2` と新しいコマンドにだけ足す。
  0 件の rc・unsupported の区別・`next_actions` (最大 3 個)・予算付きの serializer もここが正本
  - 例外の前例: [937](937-feat-dupscan-go-test-call-owner.md) は既存の callers の text と JSON の `caller` を変更し、README / 回帰テストに明記して実装した。
    既存の出力を変える案は、この形 (明記して変える) か v2 のどちらかに載せる
- [927](../927-feat-dupscan-agent-context.md): context での予算の配分・省略・継続取得 (cursor)
- [928](../928-feat-dupscan-agent-onboarding.md): 導入案内・能力診断・commandmeta の registry。hook から全 repo の解析や build を起こさない
- [921](../../890/pending/921-feat-dupscan-explore-def-refs-and-grep-nudge.md): grep 案内 hook の効果を 2026-11-25 ごろに測り直す

## アイデア

### 1. grep案内hookを対象言語へ合わせる（[939](939-ux-dupscan-go-grep-nudge.md)へ切り出し）

- **実例 (2026-10-26)**: dotfiles の `src/dupscan` (Go) で、dir を対象に `BuildDefinitions` を grep したとき、hook が `dupscan def BuildDefinitions` / `refs` を案内した。
  Go の def / refs は未対応なので、従うと 1 回無駄打ちになる (def は「go のファイル N 個は def / refs の対象外」と注記を返すので、理由は出力で分かる)
- 939実装前の判定 (`_claude/hooks/suggest-dupscan-for-symbol-grep.sh`): repo が `.swift` を 1 つでも追跡していれば候補にする。対象の言語は一部だけ見ている
  (`.swift` / `-t swift` の指定は Swift と読む。名指しした対象が全部 .swift 以外の実在ファイルなら黙る。Grep ツールの glob / type が Swift 以外なら黙る)。
  当時見ていなかったのは、dir の指定や Go の指定 (`--include=*.go`・`-t go`) を Go と判定すること
- 案: Go の指定が明示されている grep では、Go で使えるコマンドだけを案内する (Go は index (`-name` / `-returns` / `-file`)・callers・report・gate・near・explain が使える。
  Go の callers は937でテスト名も表示するようになった。`index -file` は引き続きテスト関数を載せないため、案内ではこの違いを区別する)
- 一覧を hook に写すと、928 の registry と食い違う。ただし bash の hook が registry を引くには `dupscan capabilities` を起動するしかなく、
  今の wrapper は build を起こしうる (928 の「hook から build を起こさない」に反する)。**928 の capabilities と `-no-build` が実装されるまでは、registry から引く形にはできない**。
  それまでは、hook の中の短い対応表を 928 の実装時に置き換える前提で持つ
- 馴染み: 928 の「Claude は既存 921 の hook を活用し、無差別に別 hook を重ねない」に沿う (新しい hook を作らず既存を直す)
- 時期: 921 は 2026-11-25 ごろに、Swift のパス (`.swift`・`Sources`・`Tests`・`shared/`・`macOS/`) を対象にした grep と dupscan の回数を比べる。
  Goの指定だけを変えても、旧集計のdupscan回数にはGo向け利用が混ざりうる。939ではSwiftの既存案内とstampを維持し、再測定でGo向け/unknownを区別する。**dirの中身から判定してSwiftのパスへの案内を変える案は、測り直しの後に回す**

### 2. 新しいコマンドの text は、1 行目に要約を出す

- 件数・近似かどうか・省略の有無・解析できなかったファイルの有無を 1 行目に置き、agent が全文を読まずに「続きを読むか・次を打つか」を決められるようにする。
  中身は 926 の v2 envelope (`result` / `analysis` / `output`) と同じ状態を text で書いたもので、別の状態を作らない
- 制約: 対象は新しいコマンド (927 の context・925 の search など) の text。既存コマンドの text に足すなら、937 と同じく README / fixture に明記して変える形を採る

### 3. 新しいコマンドの text は、同じファイルのヒットをまとめてパスを繰り返さない

- refs / callers は同じファイルの行が続くことが多く、行ごとに repo 相対のパスを繰り返している。ファイルを見出しにして、行は `line:col` と本文だけにする
- 制約は 2 と同じ。減る bytes は 928 の評価で測る。パスが行に無いと、agent が 1 行だけ抜き出して使うときに困る可能性があるので、採否は測ってから決める

### 4. 見つかったときにも次の一手を 1 つ返す

- 926 の `next_actions` を、0 件・unsupported だけでなく found のときにも使う (例: def で定義が 1 つに決まったら、その名前の callers の argv)。上限 3 個と「自動実行しない」は 926 のまま
- 馴染み: 926 の envelope は状態に関わらず `next_actions` を持つので、新しい欄は要らない。生成の規則を 926 と別に持たない

### 5. 既定で返す量を「判断に足る最小」から始める

- 927 の context の既定は「本体の先頭 80 行・12,000 bytes」。変更前の影響を見る用途では、署名・doc の 1 行目と refs / callers の件数だけで次の判断ができる場面が多い可能性がある
- 案: 既定を短くし、本体は `-sections` で取りに行く形を 928 の評価で比べる
- 関係: **927 の既定値と食い違う提案**。どちらが正しいかは測るまで分からないので、927 の既定は変えず、928 の評価の比較対象に入れる

## 却下した案 (再提起しない)

- hook の案内に「その名前で今引いたら何件あるか」を添える: 案内のたびに解析が走る。928 の「hook から全 repo の解析や build を起こさない」と衝突する
- 0 件の rc=1 が Bash ツールで失敗に見える問題をここで扱う: 926 は legacy の text の def / refs の rc=1 を意図して残し、v2 と新しいコマンドでは 0 件を rc=0 にする設計を持つ。
  agent を v2 / 新しいコマンドへ誘導すれば解消するので、ここでは別の手を重ねない (legacy の rc を変える案は 926 の決定と衝突する)

## 進捗

- 2026-10-26: 起票。926 / 927 / 928 / 921 / 937 と照合し、競合 (1 は 928 の未実装に依存・Swift のパスに効く案は 921 の測り直しの後・5 は 927 の既定値と食い違う) と制約を本文に書いた。実装・効果測定は未着手
- 2026-10-26: opus の読み取り専用の反証レビュー。P2 4 件 (hook の判定の説明が言い過ぎ・registry を hook から引くには 928 の未実装の `-no-build` が要る・legacy の rc=1 は 926 が残す・937 が既存の出力を変える前例と golden の不在) と
  P3 4 件 (予算の serializer の正本は 926・Go で使えるコマンドの列挙が狭い・921 の数え方は Swift のパスだけ・json-v2 に「1 行目」は無い) を、実物で確かめてすべて直した

- 2026-10-27: `docs(dupscan): refresh agent UX assumptions after test owner support`で937への参照を完了位置へ合わせ、Go callersはテスト名を表示できるという実装済みの前提へ更新した。938のhook/UX機能自体は未着手のまま。

- 2026-10-27: 案1を939へ切り出して実装した。Go向けindex/callers案内・通知間隔・build非起動を検証。案2〜5と効果測定は未着手で継続する。旧dupscan全件集計にはGoの利用も混ざりうるため、影響しないという断定を訂正した。

## 案2〜5の対応（2026-10-27）

- 案2: `context` / `evidence` の1行目に近似方法・省略・構文解析失敗ファイル数・未対応言語の有無を追加した。省略は件数/ソース行/文字数制限とcontextで選ばなかった非空の節を含む。`queryUX`を両コマンドで共有する。926のenvelope/serializerは未実装であり、共通状態との統合は926で行う。ここに独立したv2を作ってはいない。
- 案3: contextのrefs/callers、evidenceの各行を、連続する同じファイルの引用符付き見出しにまとめた。contextではline:colを保持し、同じ行の複数出現を潰さない。所有者・prod/test・参照の役割・本文・docの一致行数・呼び出し名は残す。1行を抜き出す際は直前のファイル見出しも必要になる。
- 案4: `kind` / `reason` / `argv` の構造で最大1個をtextの `next_action:` 行へ出す。自動実行せず、絶対パスの元scope・複数scope・空白/記号を含む引数を保持する。contextは省略した定義や参照、またはcallersへ進み、evidenceは呼び出し名のcallers、docだけならSwift context / Go indexへ進む。0件、名前の候補がない文字列一致、すでに全件表示した参照だけの結果には案内しない。926の上限3個以内で、将来envelopeへ載せ替える最小モデル。
- 案5: `context -sections summary` と `-sections definition,refs,callers` の節選択を追加した。summaryは署名・docの最初の行・件数・場所だけを返す。型の種類も残す。本体/型メンバーの取得argvを返し、同じscopeへ展開できる。**既定はdefinition,refs,callers / max-lines=80のまま**。927の12,000bytes予算とcursorはまだ実装していない。

### 出力量の比較と採否

変更前は626a033cのバイナリ、変更後は本対応のバイナリ。いずれも同一snapshot、既定limit=20でstdoutのUTF-8 bytesを数えた。入力は924の検証用snapshot（Obaket: 1,669 Swift files、source 4eefca272448、path/content SHA256 `1143d229d1d08a58837363ee4c0e1d1676cf939fcb870ec167f7307bc10ca0b6`、doctorは同じ924検証snapshot）。各コマンド・問い合わせ・scopeを固定し、後段を含む開発課題ではなく出力の局所比較を行った。

| コマンド / 問い合わせ | 変更前bytes | 通常表示bytes | summary bytes | summary+定義展開bytes |
|---|---:|---:|---:|---:|
| context / retrying | 12,901 | 11,485 | 1,720 | 6,020 |
| context / TransferRetryCoordinator.retry | 11,244 | 9,872 | 1,274 | 3,091 |
| context / StorageLocation | 10,608 | 9,961 | 1,020 | 2,465 |
| context / notFound938 | 897 | 1,013 | 809 | — |
| evidence / retry | 15,246 | 13,531 | — | — |
| evidence / cache | 7,354 | 6,969 | — | — |

Obaketの4例とevidence retryはSwift snapshot全体、evidence cacheだけdoctor。ヒットのある通常表示5例では約5.2〜12.2%減った。0件は状態情報の追加で116bytes増えた。短いパスや1件だけなら見出し/案内の固定費で増える可能性もあり、常に短くなるとは言わない。

summaryはヒットのある3例で通常表示より約85.0〜89.8%短い。一方、本体を読んで変更する課題はsummaryだけでは解けない。summary+定義展開にはrefs/callersの各行が含まれず、通常表示と同じ情報量の比較ではない。全情報を後から取り直すなら往復と重複した要約の費用が増えるため、既定短縮は採用しない。summaryを候補確認用の明示オプションとして採用し、実課題での総context/tokens・正しい再利用・不要な重複追加・総時間/費用は928で測る。

Swift snapshotは既存parserが57ファイルに構文解析失敗を報告し、doctorは0。前後で同じ欠落条件であり、今回解析coverage自体は改善していない。完全な件数や型解決済みの結果として扱わない。

### 検証と改善の評価

- `make -C src/dupscan lint test`: lint / 全raceテストを検証。新規回帰は同一行の複数col・test/prod・role/owner・文字数省略・不正sections・0件/複数定義・summaryから同scopeへの展開・複数scope・記号を含むargvの再実行を対象にした。
- 変更前との互換比較: Swift retryingのdef/refs/callers、Go doctor cacheのcallers、Go doctorのindexをtext/jsonで各1回、計10条件。stdout/stderr/rcがbyte一致。legacy全入力の互換性を証明するものではない。
- `dupscan gate -base 626a033c -strict-analysis src/dupscan`: 新しい重複なし、追加除外なし。全srcには既知の意図的な構文不正fixtureがあるため、strictな解析確認はdupscan moduleへ限定した。
- subagentを表示保持/省略、次操作/引数安全性、context/文書の観点で分けて反証レビューした。省略が参照だけでも定義を再取得する誤誘導、完全な参照結果の同一argvループを修正し、取得先の内容と終了を検証する回帰を追加した。単に案内の文字列があることだけのassertでは検出できなかった問題を防ぐ。

改善は「ヒット行の情報を保ちながらパスの繰り返しを減らす」「先頭で欠落条件を判断できる」「元scopeを失わず次の取得へ進める」の3点。代償は表示model/節選択/回帰テストの追加、行だけのコピーができなくなること、0件出力の増加。開発全体のtokens削減と重複定義の減少は未検証であり、達成済みとしない。

- 2026-10-27: `feat(dupscan): add concise query views and scoped next actions`で案2〜4と案5の任意summaryを実装。`docs(dupscan): close agent UX ideas with measured tradeoffs`で実測と既定維持の判断を記録する。案1は939で完了、案2〜4実装/局所評価、案5採否まで決着したため938をdoneへ移す。926/927/928のenvelope・予算/cursor・registryと開発全体の効果検証は各issueで継続。
