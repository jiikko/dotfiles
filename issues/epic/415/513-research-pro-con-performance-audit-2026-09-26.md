# 513 (research): pro-con の性能の悪そうな箇所の監査 (2026-09-26)

起票日: 2026-09-26

親: [415](415-design-claude-pm-worker-orchestration.md)

## 概要

ユーザーの依頼 (2026-09-26、カード C-071): 「pro-con でパフォーマンスの悪そうな箇所を探して issues ディレクトリに書き出して」。

pro-con (`src/pro-con` と、それが使う `src/tuikit` / `src/procsup`) の性能の悪そうな箇所を探し、見つけたものを issue に書き出す。
**監査なのでコード (`src/`) は直さない** (ほかのカードとぶつからないように)。形は 479 (リソースリークの監査) に倣う。

## やること

- 観点 (例): dispatcher の Tick ごとの仕事 (ファイルの読み直し・外部コマンドの起動・JSON の解析)、画面の描画 (フレームごとの割り当て・文字幅の計算・
  全カードの走査)、transcript・events.jsonl・runs のように伸び続けるファイルの読み方、socket の知らせの頻度と大きさ、見張り・取り込みの係・要約の係が起こす
  `claude -p` と `git` の回数
- 🚨 **性能の主張は計測で裏を取る** (perf-claims-need-measurement)。「遅そう」だけで issue にしない。
  計測は module を `mktemp -d` へ写した中の go test / benchmark で行い、本物の pro-con・本物の state dir・本物の claude の session には触らない
  (本物に対しては ps・ファイルの大きさを読むだけ)
- 走査した母集合を機械で数えて本文に書く (479 と同じ)。見つけたものを「issue にしたもの / 記録のみ / 却下」に分けて全数を勘定する
- 1 件 1 issue で起票する (`perf` の型)。issue にした番号と、記録のみにした理由をこの本文に書き戻す

## 既知のもの (重ねて起票しない)

- 478 cards.json が終えたカードも持ち続け毎回読み直す / 494 アニメのフレームの割り当てと GC /
  502 claude agents の一覧を画面と dispatcher がそれぞれ起こす / 503 transcript の末尾を毎回読み直す / 504 relay が 83KB のフレームを秒 8 回書く
  (どれも `issues/epic/415/done/`)
- 479 (リソースリークの監査) の「記録のみ」

## 関連

- 479 (リソースリークの監査。形の前例) / 460 (脆さの監査)
