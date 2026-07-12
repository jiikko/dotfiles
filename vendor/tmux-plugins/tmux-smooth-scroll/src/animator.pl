#!/usr/bin/env perl
# Smooth scroll animation with easing
use strict;
use warnings;
use Time::HiRes qw(usleep);
use POSIX qw(ceil);
use Fcntl qw(:flock); # [dotfiles patch] 終了時の状態更新を arbiter.pl と同じ lock で直列化
use constant PI => 3.14159265359;

my ($base_delay, $lines, $direction, $mode, $target_pane, $state_file, $my_gen) = @ARGV;

# [dotfiles patch] copy-mode 離脱レースで send-keys -X が失敗したときのエラーが
# run-shell の出力として tmux メッセージに出ないよう、子プロセスの stderr を捨てる
# (アニメは fire-and-forget であり、失敗は下のループ打ち切りで扱う)。
open(STDERR, '>', '/dev/null');

# [dotfiles patch] 世代打ち切り: scroll.sh が押下ごとに state_file の gen を進める。
# 自分の gen と一致しなくなったら「新しい押下に追い越された」ので即座に止まる
# (nvim の dotfiles/smooth_scroll.lua の generation と同じ仕組み)。
sub interrupted {
    return 0 unless defined $state_file && length $state_file
                 && defined $my_gen && length $my_gen;
    open(my $fh, '<', $state_file) or return 0;
    # 共有ロックで読む: arbiter.pl の truncate→write の途中 (一瞬の空ファイル) を読むと
    # gen 不一致を見逃して余分なフレームを送るため、writer の LOCK_EX を待ち合わせる
    flock($fh, LOCK_SH);
    my $line = <$fh> // '';
    close($fh);
    my ($gen) = split ' ', $line;
    return (defined $gen && $gen ne $my_gen) ? 1 : 0;
}

# Easing functions - return velocity factor (higher = faster, lower delay)
sub linear {
    return 1.0;
}

sub sine {
    my $t = shift;  # progress 0.0 to 1.0
    # Sine curve: slower at edges, faster in middle (0.3x -> 3x range)
    my $velocity = sin($t * PI);
    return 0.3 + $velocity * 2.7;
}

sub quad {
    my $t = shift;  # progress 0.0 to 1.0
    # Quadratic ease in-out: slow start/end, aggressive middle (0.2x -> 3x range)
    my $velocity;
    if ($t < 0.5) {
        $velocity = 2 * $t * $t;
    } else {
        $velocity = 1.0 - ((-2 * $t + 2) ** 2) / 2;
    }
    return 0.2 + $velocity * 2.8;
}

# Calculate delay for each step (inverse of velocity)
sub get_delay {
    my ($i, $total, $mode) = @_;
    my $t = ($total > 1) ? $i / ($total - 1) : 0.0;  # Progress: 0.0 to 1.0
    
    my $velocity;
    if ($mode eq 'linear') {
        $velocity = linear();
    } elsif ($mode eq 'quad') {
        $velocity = quad($t);
    } else {  # sine (default)
        $velocity = sine($t);
    }
    
    # Scale delay inversely with line count to maintain consistent animation duration
    # More lines = faster per-step, fewer lines = slower per-step
    my $scale_factor = 1.0;
    if ($total > 0 && $total < 10) {
        # Smooth curve: 1 line = 3x slower, 10 lines = 1x (normal speed)
        $scale_factor = 1.0 + (2.0 * (10 - $total) / 9);
    }
    
    # Higher velocity = shorter delay
    return ($base_delay * $scale_factor) / $velocity;
}

# Execute animation
my @cmd = ("tmux", "send-keys");
push @cmd, "-t", $target_pane if defined $target_pane && length $target_pane;
push @cmd, "-X", "scroll-" . $direction;

for (my $i = 0; $i < $lines; $i++) {
    # [dotfiles patch] 新しい押下に追い越されたら残りフレームを捨てる
    last if interrupted();
    # [dotfiles patch] send-keys の失敗 (copy-mode 離脱・pane 消滅) でも打ち切る
    last if system(@cmd) != 0;

    # Don't delay after last scroll
    if ($i < $lines - 1) {
        my $delay = get_delay($i, $lines, $mode);
        usleep(ceil($delay));
    }
}

# [dotfiles patch] 終了時は anim_until を 0 に戻す (自分がまだ現世代のときだけ。
# 追い越されていたら新しい押下側の状態に触らない)。gen の確認と書き込みは
# arbiter.pl と同じ flock の下で行い、並行する押下の状態更新を潰さない。
if (defined $state_file && length $state_file && defined $my_gen && length $my_gen
    && open(my $fh, '+<', $state_file)) {
    flock($fh, LOCK_EX);
    my ($gen, $last, $until) = split ' ', (<$fh> // '');
    if (defined $gen && $gen eq $my_gen && defined $last) {
        seek($fh, 0, 0);
        truncate($fh, 0);
        print $fh "$gen $last 0\n";
    }
    close($fh);
}
