import { atom, read, update } from 'claude-code'
import type { EngineInterface, Register } from 'claude-code'

import type { InboxItem } from '../types'

// 帯に出す件数と、溜めておく上限。
const SHOWN = 3
const KEPT = 50

const items = atom({ plugin: 'peer-inbox', key: 'items' } as const, [] as InboxItem[])
const isHidden = atom({ plugin: 'peer-inbox', key: 'isHidden' } as const, false)
// 回数は帯の履歴 (上限 KEPT) と別に持つ (履歴から数えると上限を超えたところでずれる)。
// 既読・未読は持たない: 目的は他セッションとの往来を本文 (メインの会話) の外に出すことで、読んだかどうかの管理は要らない
const received = atom({ plugin: 'peer-inbox', key: 'received' } as const, 0)
const sent = atom({ plugin: 'peer-inbox', key: 'sent' } as const, 0)

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

const countsText = (r: number, s: number) => `📨 受信 ${r}・送信 ${s}`

// status line に回数を常に出す (往来が 1 回も無いうちは出さない)
async function showCounts($: EngineInterface) {
  const r = (await read($, received)) ?? 0
  const s = (await read($, sent)) ?? 0
  $.ui.status(r + s > 0 ? countsText(r, s) : undefined)
}

async function push($: EngineInterface, item: InboxItem, counted: boolean) {
  await update($, items, list => [...(list ?? []), item].slice(-KEPT))
  if (item.dir === 'in') {
    await update($, isHidden, () => false)
  }
  // update の第 2 引数は atom をそのまま渡す (三項演算子で選ぶと、state の検査が読めず読み込みで止まる)
  if (counted && item.dir === 'in') {
    await update($, received, n => (n ?? 0) + 1)
  } else if (counted) {
    await update($, sent, n => (n ?? 0) + 1)
  }
  await showCounts($)
}

export const register: Register = on => {
  // 読み直し (hot reload・再起動) の後も、数えた回数を status line に出し直す
  on('session.start', async ($, e, next) => {
    const r = await next(e)
    await showCounts($)
    return r
  })

  on('session.receive', async ($, e, next) => {
    // subagent 宛 (e.agentId あり) は main の会話ではないので帯に出さない。
    if (e.agentId === undefined && PEER_KINDS.has(e.origin.kind)) {
      const who = 'teammate' in e.origin ? e.origin.teammate : e.origin.kind
      const text = oneLine(e.text)
      await push($, { dir: 'in', who, text, at: await $.clock.now() }, true)
      $.ui.toast(`📨 ${who}: ${clip(text, 80)}`, { timeoutMs: 8000 })
    }
    return next(e)
  }).catch(($, e, next) => (next.called ? next(e) : next(e)))

  on('session.send', async ($, e, next) => {
    if (e.agentId !== undefined) return next(e)
    const r = await next(e)
    // 送信の回数は届いたものだけを数える (帯には届かなかったものも残す)
    await push($, { dir: 'out', who: e.to, text: oneLine(e.text), at: await $.clock.now() }, r.isDelivered)
    return r
  }).catch(($, e, next) => (next.called ? next(e) : next(e)))

  on('ui.render', { component: 'AbovePrompt' }, async ($, e, next) => {
    const list = await read($, items)
    if (e.props.hasSurvey || list.length === 0 || (await read($, isHidden))) {
      return next(e)
    }

    const { Box, Button, Text } = $.ui.resolve(e)
    const width = e.props.bodyColumns
    const r = (await read($, received)) ?? 0
    const s = (await read($, sent)) ?? 0
    const shown = list.slice(-SHOWN)
    const prefixWidth = 'HH:MM ⇦ '.length

    return (
      <Box flexDirection="column" width={width}>
        <Box flexDirection="row" justifyContent="space-between">
          <Text bold color="subtle">
            📨 他セッション 受信 {r}・送信 {s}
          </Text>
          <Button key="hide" label="隠す" hotkey="h" onPress={() => update($, isHidden, () => true)} />
        </Box>
        {shown.map((i, n) => {
          const arrow = i.dir === 'in' ? '⇦' : '⇨'
          const whoWidth = Math.min(16, i.who.length)
          const body = clip(i.text, Math.max(8, width - prefixWidth - whoWidth - 2))
          return (
            <Box key={`${i.at}-${n}`} flexDirection="row">
              <Text dimColor>{hhmm(i.at)} </Text>
              <Text color={i.dir === 'in' ? 'success' : 'claude'}>
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
