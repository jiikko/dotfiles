# tests/pro-con

pro-con の e2e モード (`bin/pro-con e2e start <置き場>`) を使うテスト。本物の dispatcher と画面を、置き場ごとの隔離 tmux サーバで動かし、
PG / PM は台本どおりの偽物 (claude を起動しない)。共通部品は `lib/e2e_helper.sh`。

| ファイル | 確かめること | 所要 | どこで走るか |
|---|---|---|---|
| `test_e2e_scenario.sh` | 依頼 → 質問 → 回答 → テストの係 → レビュー → 終了の通し | 数秒 | `make test` / CI |
| `test_e2e_multi_screen.sh` | 2 画面: B を閉じても dispatcher は残り、最後の A を閉じると止まる | 数秒 | `make test` / CI |
| `test_e2e_multi_screen_dispatcher_killed.sh` | dispatcher を kill -9 した後でも、B は止めず最後の A が止める | 数秒 | `make test` / CI |
| `e2e_screen_killed.sh` | 画面を kill -9 → 約 60 秒後に dispatcher が偽の PG を止めて抜ける | 約 64 秒 | `make test-pro-con-slow` / CI |

- `e2e_*.sh` は 1 分以上かかるので `make test` の自動収集 (`test_*.sh`) から外してある。CI の `src_pro-con.yml` の e2e job は両方を走らせ、
  そこでは skip (77) も失敗と数える
- go / tmux / jq が無ければ skip (77) する (Tests の rest の腕には go が無い)
