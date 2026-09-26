#!/bin/bash
# sample.py の見本を .ans に書き出す (issue 537。使い捨て)
set -eu
cd "$(dirname "$0")"
for s in A B C; do
	python3 sample.py "$s" >"row-$s.ans"
	python3 sample.py "$s" --width 70 >"row-$s-narrow.ans"
done
python3 sample.py A --confirm X >confirm-X.ans
python3 sample.py A --confirm Y >confirm-Y.ans
