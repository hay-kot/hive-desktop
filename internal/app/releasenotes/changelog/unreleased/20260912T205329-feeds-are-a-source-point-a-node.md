---
kind: added
---

**Feeds are a source.** Point an **RSS feed** node at an RSS, Atom or JSON
Feed URL and its entries arrive in the inbox like anything else -- a blog, a
project's releases, a status page. Each fetch sends the feed's `ETag`, so an
unchanged feed costs one round trip and no parse. Public feeds only for now:
the node sends no token. Nothing is archived when an entry scrolls off the end
of the feed, because that means it is old, not finished.
