#!/bin/bash
# sample.py の見本を .ans に書き出す (issue 543。使い捨て)
set -eu
cd "$(dirname "$0")"
for p in A B C; do
	for s in run stop; do
		python3 sample.py "$p" "$s" >"$p-$s.ans"
	done
done
