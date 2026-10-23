# serial-run の順番待ちに make の入口の target を包む共有の断片。
# 他の repo へ組み込むガイドは docs/tools/serial-run.md。
# 使い方: Makefile で include し、重い入口の recipe の先頭に $(serial_run) を置いて、本体を別の target に分ける:
#
#   include $(HOME)/dotfiles/make/serial-run.mk
#   test:
#   	@+$(serial_run) $(MAKE) --no-print-directory test-all
#
# 包むと、人の端末・スクリプト・サブエージェント・codex のどこから呼ばれても、マシン全体で 1 本ずつ流れる
# (Claude Code の hook は Bash ツールが打った字面しか包めない)。
#
# 包まない (空になる) とき:
# - CI がある: runner は 1 台に 1 本で、待ち列が要らない
# - SERIAL_RUN_MAKE_DISABLE がある: 人が外すとき (serial-run の入口のビルドが壊れて 125 で落ちる日の逃げ道も兼ねる)
# - make が -n / -q / -t で呼ばれた: `+` の行は dry-run でも走るので、包むと何も実行しない呼び出しが本物の列に並ぶ
# - 本体 checkout の serial-run が無い (dotfiles を入れていない機械)
# 🚨 入れ子 (もう serial-run の中: hook が包んだ make test・中の make) は make 側で判定しない。serial-run が札の番号・nonce・lock を
#    照合して素通しにする (SERIAL_RUN_HELD の有無だけで見ると、別の lock の中・漏れて残った値でも素通りする)
# 🚨 呼ぶのは worktree の版ではなく本体 checkout の serial-run に固定する。待ち列の置き場は 1 つなので、札の形式が版で違うと食い違う
SERIAL_RUN_BIN ?= $(HOME)/dotfiles/bin/serial-run
# 1 文字のフラグの並び。make 3.81 は MAKEFLAGS を " --no-print-directory -sn -- VAR=n" の形で持ち、新しい make は 1 語目に "sn" を置く。
# 1 文字のフラグだけを採る: 3.81 の形は `-` 1 つで始まる語 (長いフラグ --… と、`--` の後の変数の代入 VAR=n は採らない)、
# 新しい make の形は `-` で始まらず `=` を含まない 1 語目。変数の代入の n を -n と読まない
serial_run_flags = $(filter-out --%,$(filter -%,$(MAKEFLAGS))) $(foreach w,$(filter-out -%,$(firstword $(MAKEFLAGS))),$(if $(findstring =,$(w)),,$(w)))
serial_run_noexec = $(findstring n,$(serial_run_flags))$(findstring q,$(serial_run_flags))$(findstring t,$(serial_run_flags))
serial_run = $(if $(CI)$(SERIAL_RUN_MAKE_DISABLE)$(serial_run_noexec),,$(if $(wildcard $(SERIAL_RUN_BIN)),$(SERIAL_RUN_BIN) --))
