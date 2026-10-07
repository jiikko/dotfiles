// 長く走っている tool 呼び出しの経過時間を、プロンプトの上の帯に出す。
// 走っている呼び出しの表は module の変数 (Map) に持つ: tool.call の前後と 1 秒の timer が同じ worker で同期に読み書きするので、
// $.state の CAS (update) で競合させない (red team: 並列の終了で cleanup が失敗して記録が残る / 空の snapshot で timer を止める /
// 重なった tick が同じ toast を 2 回出す、の 3 件が全部その競合から出た)。hot reload で module が作り直されると await 中の
// 記録は消えるが、その呼び出しの finally も走らないので、残骸は出ない。$.state は帯を描き直す合図 (tick) だけ。
// 5 分ごとに toast (「codex review: 10 分経過」) も出す。待ちの上限を人が判断する材料で、何かを止めはしない。
import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register, Timer } from 'claude-code'

import { SHOW_AFTER_MS, clip, dueToast, formatElapsed, labelOf, visible } from './elapsed'
import type { Run } from '../types'

const tick = atom({ plugin: 'tool-elapsed', key: 'tick' } as const, 0)

const runs = new Map<string, Run>()
let timer: Timer | null = null
let ticking = false

async function onTick($: EngineInterface): Promise<void> {
  if (ticking) return // 前の周がまだ終わっていない (toast の 2 重出しを防ぐ)
  ticking = true
  try {
    if (runs.size === 0) {
      await update($, tick, n => (n ?? 0) + 1) // 最後の 1 本が終わった帯を空に描き直してから止める
      timer?.cancel()
      timer = null
      return
    }
    const nowMs = await $.clock.now()
    for (const r of runs.values()) {
      const due = dueToast(r, nowMs)
      if (due !== null) {
        r.toasts = due.toasts // await の前に進める (重なった周が同じ節目を読まない)
        $.ui.toast(due.text, { timeoutMs: 8000 })
      }
    }
    await update($, tick, n => (n ?? 0) + 1) // 読んでいる帯を描き直す
  } catch {
    // $ の口が拒否されても timer を未処理の reject にしない (次の周でやり直す)
  } finally {
    ticking = false
  }
}

function ensureTimer($: EngineInterface): void {
  if (timer !== null) return
  timer = $.clock.every(1000, () => {
    void onTick($)
  })
}

export const register: Register = on => {
  on('session.start', ($, e, next) => {
    runs.clear()
    return next(e)
  })

  on('tool.call', async ($, e, next) => {
    const nowMs = await $.clock.now()
    const id = e.tool_use_id ?? `${e.tool}-${nowMs}-${Math.random().toString(36).slice(2, 8)}`
    // tool の入力は envelope の直下に展開されている (e.command 等)
    runs.set(id, { id, tool: e.tool, label: labelOf(e.tool, e), startedAt: nowMs, agentId: e.agentId, toasts: 0 })
    ensureTimer($)
    try {
      return await next(e)
    } finally {
      runs.delete(id)
    }
  }).catch(($, e, next) => (next.called ? next(e) : next(e)))

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    await read($, tick) // 1 秒ごとの更新を購読する
    const nowMs = await $.clock.now()
    const shown = visible(runs.values(), nowMs)
    if (e.props.hasSurvey || shown.length === 0) return next(e)

    const { Box, Text } = $.ui.resolve(e)
    const width = e.props.bodyColumns
    return (
      <Box flexDirection="column" width={width}>
        <Text bold color="warning">
          ⏳ 実行中 {shown.length} 件 ({formatElapsed(SHOW_AFTER_MS)} 以上)
        </Text>
        {shown.map(r => {
          const elapsed = formatElapsed(nowMs - r.startedAt)
          const sub = r.agentId !== undefined ? ' (subagent)' : ''
          return (
            <Box key={r.id} flexDirection="row">
              <Text color="warning">{elapsed} </Text>
              <Text wrap="truncate-end">{clip(`${r.label}${sub}`, Math.max(8, width - elapsed.length - 2))}</Text>
            </Box>
          )
        })}
      </Box>
    )
  })
}
