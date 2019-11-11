#!/bin/sh

# 画像を上書きする
# 500x300以上のサイズじゃないと文字が見きれる

set -eu

if [ "$#" -ne 1 ]; then
  echo lgtm.sh input.image
  exit 1
fi

LABEL=LGTM

convert \
  -gravity South \
  -pointsize 130 \
  -resize '500x' \
  -antialias \
  -fill black -annotate +1+1 $LABEL \
  -annotate -2-2 $LABEL \
  -annotate +2-2 $LABEL \
  -annotate +2+2 $LABEL \
  -annotate -2+2 $LABEL \
  -fill white -annotate +0+0 $LABEL \
  $1 $1
