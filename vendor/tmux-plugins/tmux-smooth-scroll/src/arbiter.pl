#!/usr/bin/env perl
# [dotfiles patch] 押下ごとの状態遷移 (gen/last_press_ms/anim_until_ms) を flock で原子化する。
#
# なぜ必要か: run-shell -b は押下ごとに scroll.sh を並行起動する。判定と書き込みを bash で
# read → 比較 → write すると、連打時に全インスタンスが「書き込み前の同じ状態」を読んで
# 全員 held=false・同一 gen になり、アニメが並行多重化する (実測: 3 連打で 3 本並行、
# 世代打ち切りも同値 gen のため不発)。ここで flock(LOCK_EX) の下で read-modify-write を
# 1 プロセスに閉じ、押下の直列化を保証する。
#
# 入力:  state_file repeat_ms anim_cap_ms
# 出力:  "GEN HELD" の 1 行 (HELD: 1=素通し(即時ジャンプ) / 0=アニメ開始)
#        状態ファイルを開けない場合は "0 0" (ガードなしアニメへ縮退)
use strict;
use warnings;
use Time::HiRes qw(time);
use Fcntl qw(:flock O_RDWR O_CREAT);

my ($state_file, $repeat_ms, $anim_cap_ms) = @ARGV;
my $now = int(time() * 1000);

my $fh;
unless (sysopen($fh, $state_file, O_RDWR | O_CREAT)) {
    print "0 0\n";
    exit 0;
}
flock($fh, LOCK_EX);

my $line = <$fh> // '';
my ($gen, $last, $until) = split ' ', $line;
$gen   = 0 unless defined $gen   && $gen   =~ /^\d+$/;
$last  = 0 unless defined $last  && $last  =~ /^\d+$/;
$until = 0 unless defined $until && $until =~ /^\d+$/;

# held = 前回押下から repeat_ms 未満 (キーリピート/連打)、またはアニメ進行中の再押下。
# どちらも「アニメせず素通し」+「gen を進めて進行中アニメを打ち切る」に倒す
# (nvim の dotfiles/smooth_scroll.lua の handler と同じ判定)。
my $held = (($now - $last) < $repeat_ms || $now < $until) ? 1 : 0;
$gen++;

# anim_until はアニメ開始時のみ立てる。animator が終了時に 0 へ戻すが、プロセスが
# kill されて戻し損ねても cap 経過で自己回復する
my $new_until = $held ? 0 : $now + $anim_cap_ms;

seek($fh, 0, 0);
truncate($fh, 0);
print $fh "$gen $now $new_until\n";
close($fh); # flock 解放

print "$gen $held\n";
