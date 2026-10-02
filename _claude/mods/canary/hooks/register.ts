import type { Register } from 'claude-code'

// 何もしない mod。読み込まれたことを外から確かめるためだけにある (issue 619)。
// DOTFILES_MOD_CANARY_DIR が在るときだけ、そこへ印 `loaded` を書く。読まれる経路の実測
// (対話 / claude -p / pro-con の PG) は、経路ごとに新しいディレクトリを渡してこの印の有無で読む。
// 印の名前は固定 (経路ごとに新しいディレクトリを渡すので足りる)。session_id は中身に入れる。
export const register: Register = on => {
  on('session.start', async ($, e, next) => {
    const started = await next(e)
    const dir = await $.env.get('DOTFILES_MOD_CANARY_DIR')
    if (dir) {
      // id と、このプロセスから見える CLAUDE_CODE_PLUGIN_DIRS は、bg (claude --bg) の経路の切り分けに使う。
      // bg のセッションは daemon が先に起こした spare で、環境は daemon を起こしたプロセスのもの (client の環境ではない)
      const id = await $.session.id().catch(() => null)
      const pluginDirs = await $.env.get('CLAUDE_CODE_PLUGIN_DIRS')
      await $.fs.write(
        `${dir}/loaded`,
        JSON.stringify({ id, surface: e.surface, isInteractive: e.isInteractive, cwd: e.cwd, pluginDirs }) + '\n',
      )
    }
    $.ui.log('canary: loaded', { to: 'debug' })
    return started
  })
}
