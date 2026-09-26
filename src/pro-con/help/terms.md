# 用語 (pro-con help terms)

人か外の Claude が出した依頼を、PM がカードに分けて積み、PG がカード 1 枚ずつ自分の worktree で実装し、
取り込みの係が確かめて master へ取り込む。カードの記録を書くのは dispatcher だけで、ほかの全員は受付の箱に依頼を置く。

## 役 (プロセス。`pro-con ps` の役の列と同じ名前)

{{役}}

## 仕組みの語

- **受付の箱** (inbox): 状態の置き場の `inbox/`。画面・`pro-con card`・`pro-con config` はここに依頼のファイルを置くだけで、
  記録 (`cards.json`) へ適用するのは dispatcher。dispatcher が居なければ箱に溜まる (画面のヘッダーに「適用待ち N 件」)。
  適用で除けた依頼は `inbox/rejected/` へ移り、理由は `pro-con log` (kind `reject`) に出る
- **状態の置き場**: `~/.local/state/pro-con/live/` (`$XDG_STATE_HOME` があればその下)。中身は `pro-con help debug`
- **カード**: 依頼 1 件 (`C-001` の形の ID。使い回さない)。issue を repo#番号で持つ。PG は 1 枚につき 1 つ
- **レーン**: カンバンの列 = カードの状態。左から順に進む (画面の `?` にも同じ表が出る)
- **追記 / 方針変更 / 別件**: 動いているカードへの追加オーダー (画面の `+`)。使い分けは `pro-con help usage`
- **持ち主の画面 / join / view**: 画面の 3 つの開き方。最後の持ち主の画面を閉じると dispatcher と PG が止まる。違いは `pro-con help usage`

## レーン

{{レーン}}

## 人の番

{{人の番}}
画面ではバッジの頭に黄の `!人の番`。tmux の件数と macOS の通知も人の番だけ。
