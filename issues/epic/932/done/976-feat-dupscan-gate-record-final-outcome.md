# 976 (feat): gate の実行記録に最終終了結果と再現条件を残す

起票日: 2026-10-29
優先度: 高

## 問題と観測

[Obaket 1301の中間評価](https://github.com/jiikko/obaket/blob/f3e77fa65/docs/research/dupscan-evaluation-2026-10-29.md)の72run中、9runはrecordの `failed=false` だが、
`-fail-new-exclusions` による意図したrc=1だった。初回のgit互換rc=2はrecord自体が無かった。
もう1件の欠測PRはjobs/logsが取得できず、gate実行の有無も不明。両者を同じCLI障害と扱わない。
測定対象は旧CI pin3版であり、現行版の誤検知率を表す数字ではない。

現行 `a76960c23` の `gate_cmd.go` と `internal/report/gate.go` でも、AppendRecordは最終終了判定より前。
`failed` は重複違反だけを示す。strict解析不完全や出力失敗で記録後にrc=2になる場合があり、
base解決等の早期rc=2は記録まで届かない。`new_exclusions` のElementにはelement/commentだけがあり、kind/reasonは無い。
baseの解決済みSHAは既にある。headは作業ツリーなら「作業ツリー」、明示-headなら短縮SHA。
「SHAも解析不完全情報も全く無い」という課題ではない (959の情報は既存)。

Obaketでは別JSON `dupscan-run.json` でrun/attempt・head/source SHA・tool pin・最終rc・record有無を補完し、
[実GHAで成功経路を確認済み](https://github.com/jiikko/obaket/actions/runs/40882303334)。
Obaket側では補助JSONを実装済み。異常系はローカル検証で、runner消失等の回収は保証しない。
これは同じ補完を各repoで手作りしないための上流改善。

## 提案する範囲

- 既存failedの意味を変えず、recordへ最終exit_codeと複数の判定理由を追加する。
  重複違反・新規除外policy・strict解析不完全・入出力失敗は併存し得るので、単一理由だけに潰さない。
- tool revision、完全なhead commitと作業ツリー印、実効levels/閾値/除外policy等を再現条件として残す案を検討する。
  head commitを作業ツリー全体の内容証明とは扱わない。GHAのrun ID等はcaller sidecarに残す。
- record先を認識できた後の早期エラーにはphase/未取得値を残せるか設計する。
  起動前・SIGKILL・書込不能まで常に記録できるとは保証しない。書込失敗はstderr/非0で知らせる。
- 新規除外のkind/reasonを構造化して取得可能にする。要素数と除外ルール数の違いも維持する。
  JSON互換性/schemaの移行を実装前に決める。

## 受け入れ条件

- [x] 重複rc=1、新規除外のみrc=1/failed=false、strict rc=2、複数理由、早期base/configエラー、stdout失敗を区別できる。
- [x] record書込失敗・未起動/強制終了は記録欠測として扱い、成功に補完しない。
- [x] 既存failedとgate判定を維持し、必要な再現条件・除外理由をconsumerが照合できる。
- [x] 互換性と、書込の失敗が元の判定へ及ぼす扱いを回帰/変異で検証する。

## 分担

[905](../../890/done/905-feat-dupscan-ci-check-gate.md)のrecord導入の後続。
[948](../948-research-dupscan-meets-its-aims.md)の一般効果、[917](../../890/pending/917-test-dupscan-usefulness-verification.md)の1か月評価は別。
[959](959-research-dupscan-dogfood-feedback-obaket-1307-part3-2026-10-29.md)で済んだ解析不完全/origin表示の再実装はしない。

## 進捗

- 2026-10-30: 実装 (dotfiles-53)。gate の本体を `gateOnce` (判定と出力をして結果を返す) に分け、`runGate` が出力と最終の判定の後に記録を 1 回だけ書く
  - 記録の行 (`report.GateRecord`、`record_version: 2`): `exit_code` (最終の rc)・`reasons` (rc を非 0 にした理由の全部。`duplicates` / `new_exclusions` / `path_excluded_passed` / `strict_incomplete` / `output_error`、判定の前に止まったら `error`。rc が 0 のときだけ空)・`phase` と `error` (判定の前に止まった段階: args / scope / head / base / config / scan / untracked)・`tool` (実行ファイルの sha256。build に VCS の版が入っていればそれも)・`head_commit` (完全な SHA。作業ツリーでは HEAD の commit で、作業ツリーの中身の証明ではない)・`conditions` (levels / fail_levels / min_nodes / min_closure_nodes / fail_new_exclusions / strict_analysis / no_exclude / base / head に渡した値)。判定まで届いた run は前と同じ GateSummary の欄 (`failed` など) を持ち、判定の前に止まった run は持たない (成功に読ませない)
  - `new_exclusions` の各要素に `kind` と `reason` (要素を書いた除外のもの)
  - 出力の失敗: text / xcode の出力は `fmt.Fprintf` の誤りを捨てていて、**以前から閉じた stdout でも rc=0 だった**。最初の書き込みの失敗を覚える writer (sweep の `lineWriter` に `Write` を足した) で包み、`output_error` で rc=2 にした
  - `-record` を渡すと SIGPIPE を無視する (落ちると出力の後に書く記録が残らない。`| head` で切った run も `output_error` として記録する)
  - 記録を書けなければ、元の判定を stderr に出して rc=2 (`判定は rc=1。記録を書けないので rc=2`)
  - 起動前・flag の解析の失敗 (`-record` の先も読めていない)・SIGKILL の run は行が無い (欠測)。README の `-record` の行に書いた
- 検証: `make -C src/dupscan test lint` rc=0、root の `make test-lint` rc=0 (1 回目はコメントの issue 番号で落ち、外した)
- 変異 (mutate-verify-list、8 本。どれも狙ったテストが赤): 出力の失敗を読まない / new_exclusions の理由を積まない / strict の理由を積まない / 早期の phase を入れない / 除外の要素に kind を入れない / 記録の失敗で元の rc を返す / SIGPIPE を無視しない / 早期の reasons を空にする
- **反証レビュー 1 周目 (opus、読み取り)**: P1 なし・P2 3 件・P3 8 件。採ったもの: ①P2 SIGPIPE で記録が消える (記録を出力の後にしたことで以前より悪化) → `-record` のときは無視し、子プロセスと本物のパイプのテストで固定 ②P2 早期に止まった run の reasons が空で rc=2 と食い違う → `["error"]` ③P2 知らない `-format` で判定の欄と phase が同じ行に出る → 形式の検査を判定の前へ ④P3 head_commit が早期のエラーで欠ける → head を決めた時点で取る ⑤P3 sha256 を書く時点で読むと go_autobuild の rename で別の実行ファイルを読む → 起動時に取る (窓は狭くなったが無くならない) ⑥P3 記録の版が無い → `record_version` ⑦P3 記録の失敗で元の rc を出すテストが無い → 足した
  - 採らなかったもの: 作業ツリーの印 (追跡中のファイルの変更の有無) は足さない (`untracked_files` はあり、作業ツリーの中身の証明は `head_commit` の役割の外と README に書いた)。`capabilities -outputs` に記録の型を載せない (962 の walker がポインタの埋め込みを辿れない。記録の欄は README と `GateRecord` のコメントが正本)。reason の名前 `path_excluded_passed` は GateSummary の欄と同じ綴りだが、値と欄名なので衝突しない
- **反証レビュー 2 周目 (直した差分。opus、読み取り)**: P1 1 件・P3 4 件。①P1 rebase で取り込んだ他人の変更 (`scopeComplete` の失敗の枝の `return 2`) が `gateOnce` の戻り値の型とぶつかり、ビルドが落ちていた → `stop("scope", …)` にした ②P3 BaseIsHead が HEAD を記録と別に解決し直していた → 記録の `head_commit` と同じ値で比べる ③P3 出力の switch の既定が text で、形式の一覧と出力がずれると黙って text を出す → 既定は判定不能にした
  - 採らなかったもの: SIGPIPE の無視が git の子プロセスへ継承される形 (未確認。dupscan が git の出力を途中で閉じる経路は見つからなかった)。3 周目は回していない (直しは早期 return 1 か所・比べる値・switch の既定で、どれもビルドとテストで見える)
