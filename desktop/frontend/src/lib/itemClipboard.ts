import { githubPayload } from './feedPresentation'
import type { InboxItem } from '../types/feed'

// Clipboard text for an inbox item's "Copy contents" action: a compact header
// (title, repo #num, link) followed by the raw markdown body — self-contained
// enough to paste into a session prompt or a message without the reader
// needing the app open.
export function itemContents(item: InboxItem): string {
  const github = githubPayload(item)
  const reference = [github.repo && github.num ? `${github.repo} #${github.num}` : '', item.url].filter(Boolean).join(' · ')
  const header = [item.title, reference].filter(Boolean).join('\n')
  const body = github.body.trim()
  return body ? `${header}\n\n${body}` : header
}
