# 976 (feat): gate の実行記録に最終終了結果と再現条件を残す

> 🚨 **担当中: kojiM3MBP (glogx)**（2026-10-29〜）

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

- [ ] 重複rc=1、新規除外のみrc=1/failed=false、strict rc=2、複数理由、早期base/configエラー、stdout失敗を区別できる。
- [ ] record書込失敗・未起動/強制終了は記録欠測として扱い、成功に補完しない。
- [ ] 既存failedとgate判定を維持し、必要な再現条件・除外理由をconsumerが照合できる。
- [ ] 互換性と、書込の失敗が元の判定へ及ぼす扱いを回帰/変異で検証する。

## 分担

[905](../890/done/905-feat-dupscan-ci-check-gate.md)のrecord導入の後続。
[948](948-research-dupscan-meets-its-aims.md)の一般効果、[917](../890/pending/917-test-dupscan-usefulness-verification.md)の1か月評価は別。
[959](done/959-research-dupscan-dogfood-feedback-obaket-1307-part3-2026-10-29.md)で済んだ解析不完全/origin表示の再実装はしない。
