import { afterEach, describe, expect, it, vi } from 'vitest'
import { Terminal } from '@xterm/xterm'
import { TerminalOutputWriter } from '../terminalOutput'

const encoder = new TextEncoder()
const decoder = new TextDecoder()

function write(term: Terminal, data: string | Uint8Array): Promise<void> {
  return new Promise((resolve) => term.write(data, resolve))
}

function screen(term: Terminal): string[] {
  const buffer = term.buffer.active
  return Array.from({ length: term.rows }, (_, row) =>
    buffer.getLine(buffer.baseY + row)?.translateToString(true) ?? '')
}

describe('TerminalOutputWriter', () => {
  afterEach(() => vi.useRealTimers())

  it('keeps a pi redraw off xterm until tmux delivers its synchronized end', async () => {
    const initial = ['old response', '', '> prompt', 'footer'].join('\r\n')
    const firstFragment = encoder.encode([
      '\x1b[?2026h',
      '\x1b[3A\r',
      '\x1b[2Knew response\r\n',
      '\x1b[2Knew detail\r\n',
      '\x1b[2K',
    ].join(''))
    const raw = new Terminal({ cols: 24, rows: 4, scrollback: 100 })
    await write(raw, initial)
    await write(raw, firstFragment)
    expect(screen(raw)).toEqual(['new response', 'new detail', '', 'footer'])

    const term = new Terminal({ cols: 24, rows: 4, scrollback: 100 })
    await write(term, initial)
    const before = screen(term)
    const writes: Promise<void>[] = []
    const output = new TerminalOutputWriter((data) => { writes.push(write(term, data)) })

    output.write(firstFragment)

    expect(writes).toHaveLength(0)
    expect(screen(term)).toEqual(before)

    output.write(encoder.encode([
      '> prompt\r\n',
      '\x1b[2Kfooter',
      '\x1b[?2026l',
    ].join('')))
    await Promise.all(writes)

    expect(writes).toHaveLength(1)
    expect(screen(term)).toEqual(['new response', 'new detail', '> prompt', 'footer'])
  })

  it('recognizes synchronized markers split at every byte boundary', () => {
    const chunks: Uint8Array[] = []
    const output = new TerminalOutputWriter((data) => { chunks.push(data) })
    const frame = encoder.encode('\x1b[?2026h\r\x1b[2Kworking\x1b[?2026l')

    for (const byte of frame) output.write(Uint8Array.of(byte))

    expect(chunks).toHaveLength(1)
    expect(decoder.decode(chunks[0])).toBe('\x1b[?2026h\r\x1b[2Kworking\x1b[?2026l')
  })

  it('passes ordinary and alternate-screen output through unchanged', () => {
    const chunks: Uint8Array[] = []
    const output = new TerminalOutputWriter((data) => { chunks.push(data) })
    const data = encoder.encode('\x1b[?1049hvim\x1b[?1049l')

    output.write(data)

    expect(chunks).toHaveLength(1)
    expect(decoder.decode(chunks[0])).toBe('\x1b[?1049hvim\x1b[?1049l')
  })

  it('preserves bytes around consecutive synchronized redraws', () => {
    const chunks: Uint8Array[] = []
    const output = new TerminalOutputWriter((data) => { chunks.push(data) })
    const data = 'before\x1b[?2026hfirst\x1b[?2026lbetween\x1b[?2026hsecond\x1b[?2026lafter'

    output.write(encoder.encode(data))

    expect(chunks.map((chunk) => decoder.decode(chunk))).toEqual([
      'before', '\x1b[?2026hfirst\x1b[?2026l', 'between', '\x1b[?2026hsecond\x1b[?2026l', 'after',
    ])
  })

  it('releases an unterminated redraw instead of stalling the terminal', () => {
    vi.useFakeTimers()
    const chunks: Uint8Array[] = []
    const output = new TerminalOutputWriter((data) => { chunks.push(data) })

    output.write(encoder.encode('\x1b[?2026hpartial'))
    expect(chunks).toHaveLength(0)

    vi.advanceTimersByTime(1000)

    expect(chunks).toHaveLength(1)
    expect(decoder.decode(chunks[0])).toBe('\x1b[?2026hpartial')
  })
})
