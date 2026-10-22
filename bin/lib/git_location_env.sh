# git_location_env.sh — 継承した「git が repo の場所を決める環境変数」(GIT_DIR / GIT_WORK_TREE など) を外す
# shellcheck shell=sh  # shebang を持たない source 専用ファイル (bash / sh 用。zsh は `unset $var` を語に割らないので、1 個の名前として扱う)
#
# git_location_env_unset: rc 0 = 外した / 1 = git が一覧を返さない (外せていない。呼ぶ側が止める)。
#
# cwd の repo を相手にする道具は、起動直後にこれを呼ぶ。継承した GIT_DIR が別の repo を指すと、`git rev-parse --show-toplevel` は
# cwd を返すのに、worktree の作成や差分の取得は GIT_DIR 側の repo を読み、2 つの repo が混ざった木で動く。
# 🚨 一覧を手で写さない: git 自身の `git rev-parse --local-env-vars` を使う (Makefile の unexport と src/subproc/gitenv.go の
#    gitLocationEnv は写しで、tests/scripts/test_make_unexports_git_location_env.sh と gitenv_test.go が本物の git と突き合わせている)。
#    GIT_NAMESPACE は --local-env-vars に出ないが ref の見え方を変えるので足す (上の 2 つも足している)
git_location_env_unset() {
  _gle_vars=$(git rev-parse --local-env-vars 2>/dev/null) && [ -n "$_gle_vars" ] || return 1
  # 語の分割は IFS 次第 (呼ぶ側が IFS を変えていると 1 個の不正な名前になり、何も外さない)。失敗と、外れていないことの両方を rc に出す
  # shellcheck disable=SC2086 # 1 行 1 変数名の出力を語に割って渡す
  unset $_gle_vars GIT_NAMESPACE || { unset _gle_vars; return 1; }
  unset _gle_vars
  [ -z "${GIT_DIR+x}" ] && [ -z "${GIT_WORK_TREE+x}" ]
}
