# 932 (epic): dupscan の精度を上げる — 既存の実装へ正しく辿り着き、見落としと誤判断を減らす

起票日: 2026-10-26

> epic 932 の親 issue。[890](../890/done/890-epic-dupscan.md) は動くもの (索引・gate・Swift と Go の adapter) を作ることがゴールで、こちらはその精度を上げる (ユーザー指示 2026-10-26)。

## 目的

書く前に既存のヘルパーを探す・gate で写しを止める、の 2 つで、次を減らす。

- 見つかるはずの既存の実装を見落とす
- 「無い」と「読めなかった・問い合わせが合っていない」を取り違える
- 候補を見つけても使えるかを誤判断する

## 子 issue

| 番号 | 何をするか |
|---|---|
| [924](924-perf-dupscan-exploration-latency.md) | 探索コマンド (`index` / `def` / `refs`) の待ち時間を減らす |
| [925](925-feat-dupscan-helper-search.md) | 用途語・署名から、名前の分からない既存ヘルパーの候補を探す |
| [926](926-feat-dupscan-query-diagnostics.md) | 問い合わせの形式と、検索 0 件のときの診断を揃える |
| [927](927-feat-dupscan-agent-context.md) | agent 向けに、定義・参照・呼び出しをまとめて返す |
| [928](928-feat-dupscan-agent-onboarding.md) | Claude / Codex 共通の導入案内と能力診断 |
| [929](929-bug-dupscan-gate-lost-file-parse-errors.md) | gate が、単位を抽出できない解析失敗を通知せずに成功する |
| [930](930-feat-dupscan-candidate-call-evidence.md) | 候補の呼び出し数に、省略引数・曖昧さの根拠を付ける |

## 状態

group にまとめただけ (2026-10-26)。どれも未着手。
