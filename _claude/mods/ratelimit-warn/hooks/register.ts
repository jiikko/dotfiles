// settings の hook _claude/hooks/ratelimit-warn.sh の mod 版 (issue 658)。
//
// 枠は 2 つの出所から読み、使える観測の新しい方で判定する (hooks/limit.ts):
//   live: session.measure が運ぶ rateLimits (直近の応答ヘッダの値) を発火時刻つきで $.state に控える
//   file: statusline が書く $XDG_CACHE_HOME/glog/claude-rate-limits.json (別 session の観測も見える)
// 判定できたプロンプトでは、hook が同じ注入をしないよう印 (ENV_MARK = `<session_id>:<epoch ms>`) を打つ。
// 印を永続の値にしないのは、mod が後で落ちた・classic.* が素通しになった・子の claude -p に継承された、のどれでも
// fallback の hook まで黙らせないため (658 の codex 反証 P1)。判定できないときは印を打たず、hook に任せる。
// 子プロセスは起こさない (hook は zsh → Go → 裏の更新を毎プロンプト起こしていた)。
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import { fromLive, judge, message, parseFile, pick, statusText } from './limit'
import type { Reading } from '../types'

const live = atom({ plugin: 'ratelimit-warn', key: 'live' } as const, null as Reading | null)

async function readFile($: EngineInterface, nowMs: number): Promise<Reading | null> {
  const xdg = await $.env.get('XDG_CACHE_HOME')
  const home = await $.env.get('HOME')
  const base = xdg !== undefined && xdg !== '' ? xdg : home !== undefined && home !== '' ? `${home}/.cache` : undefined
  if (base === undefined) return null
  try {
    const text = await $.fs.read(`${base}/glog/claude-rate-limits.json`)
    return typeof text === 'string' ? parseFile(text, nowMs) : null
  } catch {
    return null // 無い・読めない = この出所は使えない
  }
}

async function current($: EngineInterface): Promise<{ reading: Reading | null; nowMs: number }> {
  const nowMs = await $.clock.now()
  const [l, f] = await Promise.all([read($, live), readFile($, nowMs)])
  return { reading: pick(l, f, nowMs), nowMs }
}

async function refreshStatus($: EngineInterface): Promise<void> {
  try {
    const { reading, nowMs } = await current($)
    $.ui.status(reading === null ? undefined : statusText(judge(reading, nowMs)))
  } catch {
    // timer / measure から呼ばれる。$ の口が拒否しても未処理の reject にしない (表示は次の機会に直る)
  }
}

export const register: Register = on => {
  on('session.start', ($, e, next) => {
    // 放置中にリセット・鮮度切れを迎えたら status を消す (プロンプトと measure の間を埋める)
    $.clock.every(60_000, () => {
      void refreshStatus($)
    })
    return next(e)
  })

  on('session.measure', async ($, e, next) => {
    const nowMs = await $.clock.now()
    // 空の rateLimits (subscription 外・まだ応答が無い) は「live の観測が無い」なので、前の控えを消す (古い超過を再注入しない)
    const r = fromLive(e.rateLimits, nowMs)
    await update($, live, () => r)
    await refreshStatus($)
    return next(e)
  })

  on('classic.UserPromptSubmit', async ($, e, next) => {
    const { reading, nowMs } = await current($)
    if (reading === null) return next(e) // 判定できない: 印を打たず settings の hook に任せる
    const v = judge(reading, nowMs)
    $.ui.status(statusText(v))
    await $.env.set('DOTFILES_MOD_RATELIMIT_WARN', `${e.session_id}:${nowMs}`) // = limit.ts の ENV_MARK (validate が literal を求める)
    const r = await next(e)
    if (!v.over) return r
    return { ...r, additionalContext: [...(r.additionalContext ?? []), message(v, nowMs)] }
  }).catch(($, e, next) => (next.called ? next(e) : next(e)))
}
