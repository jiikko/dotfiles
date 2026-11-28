# 1122 (feat): 最後の応答が英語に崩れたら Stop hook が日本語で書き直させる

起票日: 2026-11-28

- 由来: retro 1121 の提案 B

## 概要

ユーザーへの報告 (完了報告・途中経過) が英語に崩れ、「日本語にして」と言われることが繰り返し起きている。
retro 1031 (2026-11-04、同じセッションで 4 回) では auto memory `reports-in-japanese` (送る前に冒頭の 1 文の言語を見る) を置いたが、
2026-11-26 の issue 1110 のセッションでも、サブエージェントの報告を受けた後の数通が英語になり、同じことを言われた。
memory は読むかどうかが書き手に任されるので、Stop hook で最後の応答を機械で見て、英語なら書き直させる。

## 対応方針

- Stop hook を 1 本足す (`_claude/settings.json` の Stop に並べる。SubagentStop は対象にしない。サブエージェントの報告を受けた後の main の応答は Stop で見られる)。
  `issue-progress-check.sh` と同じく、`stop_hook_active` が true の続きでは何もしない (無限ループ防止)
- 既にある Stop hook `ja-writing-hook.sh` とは別物にする。あちらは `/yomiyasu` か `ja-writing check start` で登録した原稿だけを検査し、登録が無ければ何もしないので、
  登録の無い普段の報告の言語は誰も見ていない。統合するなら、登録の無い応答にも走る経路を ja-writing に足すことになり、検査の重さが毎回の Stop に乗る
- 同じ Stop で他の hook (`issue-progress-check.sh`・`rule-harvest-check.sh`) も block しうる。理由が同時に出たときの扱いと、
  block の続きの応答 (`stop_hook_active` が true) が英語のままでも止めないことの是非を、実装の前に決める
- 最後の assistant の応答の本文を取る。既存の `issue-progress-check.sh` は stdin の `transcript_path` から transcript を読んでいるので、同じ経路を使う。
  stdin に最後の応答の本文が直接載るかは、手元の claude (2.1.332) で未実測。載るならそちらを使う (実測してから決める)
- 判定: コードブロック・インラインコード・引用 (`>` の行)・URL・パスを除いた本文で、日本語の字 (かな・漢字) の割合が閾値より低ければ block し、
  「日本語で書き直す」を理由に返す。閾値と除く範囲は、本物の transcript の応答 (日本語の報告と、英語に崩れた報告の両方) で測ってから決める
  (識別子の多い日本語の報告と、サブエージェント・codex の英語の出力を引用した日本語の報告を、英語と誤判定しない値にする)
- 短い応答 (数十字以下) は判定しない (「Done.」のような 1 語の誤判定を避ける。この長さも実測で決める)
- 英語で答えるべき場面 (ユーザーが英語で書いた・英語の文を頼まれた) を誤って止めないかを確かめる。ユーザーの直前の発言が英語なら判定しない、を候補にする。
  transcript の user の行には tool_result も入るので、ユーザーが打った発言だけを取る (tool_result・hook の注入を除く) 方法を実測で確かめる

## テスト

- 日本語の報告 (識別子・コマンド・コードブロックが多いもの) は block しない
- 英語の報告は block する。`stop_hook_active` が true のときは block しない
- 実装をわざと壊して確かめる: 判定の閾値の比較を反転する / コードブロックを除く処理を外す で、それぞれテストが落ちる

## 関連ファイル

- `_claude/settings.json` (Stop の hooks)
- `_claude/hooks/issue-progress-check.sh` (transcript を読む Stop hook の前例)
- auto memory `reports-in-japanese` (hook が入ったら、memory から hook へ参照を足す)
