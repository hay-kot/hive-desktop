---
kind: changed
---

**Sources fetch in parallel.** Each poll fetches up to four sources at once instead of one at a time, so new items land sooner and the feed's Refresh button and `r` finish faster. With 16 sources across GitHub, Gitea, Grafana, and PostHog, a poll takes about 34% less time, and fetching the individual sources about 54% less.
