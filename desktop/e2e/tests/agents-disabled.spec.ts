import { expect, test } from './fixtures.js'

// experimental.agents defaults off, and serve.sh mints no per-server opt-in
// for it (unlike experimental.terminal, which every server carries — see
// serve.sh's HIVE_DESKTOP_EXPERIMENTAL_TERMINAL export). That is what lets
// this spec run against any of the harness's existing servers with zero
// plumbing of its own: it asserts the off state, which is the default.
//
// The title bar's mode group renders on "either flag is on" (ADR terminal-experimental-gate /
// ADR a-workspace-declares-its-own-authority), not on terminal specifically — this feed server carries
// experimental.terminal, so the group and its Code segment are present. What
// this guards is that the Agents segment specifically stays absent while its
// own flag is off, rather than the either-flag rule accidentally exposing it.

const feedItemCount = 6

test('the Agents segment is absent while experimental.agents is off', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByTestId('feed-item')).toHaveCount(feedItemCount)

  // The group itself renders — this server has experimental.terminal on —
  // but only the segments whose own flag is on.
  await expect(page.getByTestId('titlebar-mode-hub')).toBeVisible()
  await expect(page.getByTestId('titlebar-mode-terminal')).toBeVisible()
  await expect(page.getByTestId('titlebar-mode-agents')).toHaveCount(0)
})
