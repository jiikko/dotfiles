import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { InboxItem } from '../types'

// 帯に出す件数と、溜めておく上限。
const SHOWN = 3
const KEPT = 50

const items = atom({ plugin: 'peer-inbox', key: 'items' } as const, [] as InboxItem[])
const isHidden = atom({ plugin: 'peer-inbox', key: 'isHidden' } as const, false)

// 他セッション / teammate との往来だけを対象にする (relay の通知・cron・Remote Control は除く)。
const PEER_KINDS = new Set(['peer', 'coordinator', 'peer-send-message'])

const oneLine = (text: string) => text.replace(/\s+/g, ' ').trim()

const clip = (text: string, width: number) =>
  text.length > width ? `${text.slice(0, Math.max(0, width - 1))}…` : text

const hhmm = (at: number) => {
  const d = new Date(at)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getHours())}:${p(d.getMinutes())}`
}

async function push($: EngineInterface, item: InboxItem) {
  await update($, items, list => [...(list ?? []), item].slice(-KEPT))
  if (item.dir === 'in') {
    await update($, isHidden, () => false)
  }
  const unread = (await read($, items)).filter(i => i.dir === 'in' && !i.isRead).length
  $.ui.status(unread > 0 ? `📨 未読 ${unread}` : undefined)
}

export const register: Register = on => {
  on('session.receive', async ($, e, next) => {
    // subagent 宛 (e.agentId あり) は main の会話ではないので帯に出さない。
    if (e.agentId === undefined && PEER_KINDS.has(e.origin.kind)) {
      const who = 'teammate' in e.origin ? e.origin.teammate : e.origin.kind
      const text = oneLine(e.text)
      await push($, { dir: 'in', who, text, at: await $.clock.now(), isRead: false })
      $.ui.toast(`📨 ${who}: ${clip(text, 80)}`, { timeoutMs: 8000 })
    }
    return next(e)
  }).catch(($, e, next) => (next.called ? next(e) : next(e)))

  on('session.send', async ($, e, next) => {
    if (e.agentId === undefined) {
      await push($, { dir: 'out', who: e.to, text: oneLine(e.text), at: await $.clock.now(), isRead: true })
    }
    return next(e)
  }).catch(($, e, next) => (next.called ? next(e) : next(e)))

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const list = await read($, items)
    if (e.props.hasSurvey || list.length === 0 || (await read($, isHidden))) {
      return next(e)
    }

    const { Box, Button, Text } = $.ui.resolve(e)
    const width = e.props.bodyColumns
    const unread = list.filter(i => i.dir === 'in' && !i.isRead).length
    const shown = list.slice(-SHOWN)
    const prefixWidth = 'HH:MM ⇦ '.length

    return (
      <Box flexDirection="column" width={width}>
        <Box flexDirection="row" justifyContent="space-between">
          <Text bold color={unread > 0 ? 'warning' : 'subtle'}>
            📨 他セッション {list.length} 件{unread > 0 ? ` (未読 ${unread})` : ''}
          </Text>
          <Box flexDirection="row" gap={1}>
            <Button
              key="read"
              label="既読"
              hotkey="r"
              onPress={() => update($, items, l => (l ?? []).map(i => ({ ...i, isRead: true })))}
            />
            <Button key="hide" label="隠す" hotkey="h" onPress={() => update($, isHidden, () => true)} />
          </Box>
        </Box>
        {shown.map((i, n) => {
          const arrow = i.dir === 'in' ? '⇦' : '⇨'
          const whoWidth = Math.min(16, i.who.length)
          const body = clip(i.text, Math.max(8, width - prefixWidth - whoWidth - 2))
          return (
            <Box key={`${i.at}-${n}`} flexDirection="row">
              <Text dimColor>{hhmm(i.at)} </Text>
              <Text color={i.dir === 'in' ? (i.isRead ? 'success' : 'warning') : 'claude'} bold={i.dir === 'in' && !i.isRead}>
                {arrow} {clip(i.who, 16)}{' '}
              </Text>
              <Text wrap="truncate-end" dimColor={i.dir === 'out'}>
                {body}
              </Text>
            </Box>
          )
        })}
      </Box>
    )
  })
}
