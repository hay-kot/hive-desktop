/**
 * Routes a pane's pastes to `paste` instead of xterm's own handler, and reports
 * how to stop.
 *
 * xterm brackets a paste only once it has seen the pane's program enable
 * bracketed paste, and it cannot have: a pane is attached long after its agent
 * started, and the first paint that reconstructs the screen carries no DEC
 * private modes. Left to xterm, every newline in the paste is rewritten to a
 * carriage return and the agent reads one submission per line. The server
 * pastes through tmux instead, which knows the mode
 * (ADR pastes-are-tmux-paste-buffer-operations-not-keystrokes).
 *
 * The listener captures on the host so it runs before xterm's own, which is
 * registered on the textarea inside it.
 */
export function interceptPaste(host: HTMLElement, paste: (text: string) => void): () => void {
  const onPaste = (event: ClipboardEvent): void => {
    const text = event.clipboardData?.getData('text/plain')
    if (!text) return
    event.preventDefault()
    event.stopPropagation()
    paste(text)
  }
  host.addEventListener('paste', onPaste, true)
  return () => host.removeEventListener('paste', onPaste, true)
}
