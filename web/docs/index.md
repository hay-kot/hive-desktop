---
icon: fontawesome/brands/hive
hide:
  - toc
---

<section class="hive-hero">
  <div class="hive-hero__copy">
    <div class="hive-eyebrow">Personal automation for the agent era</div>
    <h1>Everything that wants your attention, down to one ping per change.</h1>
    <p class="hive-lede">Hive watches GitHub, your Grafana alerts, and anything that can POST a webhook, then routes them through rules your own coding agent wrote. One local queue, one ping per real change, and a config file that lives in your dotfiles.</p>
    <div class="hive-hero__actions">
      <a class="md-button md-button--primary" href="getting-started/#install">Install</a>
      <a class="md-button" href="getting-started/">Read the docs</a>
    </div>
    <div class="hive-install">
      <pre><code>curl -fsSL https://hivedesktop.com/install.sh | bash</code></pre>
    </div>
    <p class="hive-hero__note">Free and open source · MIT licensed · macOS and Linux</p>
  </div>

  <div class="hive-terminal-demo" role="img" aria-label="The Hive Desktop inbox: a column of feeds with unread counts beside a list of items">
    <div class="hive-terminal-demo__bar" aria-hidden="true">
      <span></span><span></span><span></span>
      <strong>Hive</strong>
    </div>
    <div class="hive-app-demo" aria-hidden="true">
      <div class="hive-app-demo__feeds">
        <div class="hive-app-demo__label">Feeds</div>
        <div class="hive-app-demo__feed"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M13 6h3a2 2 0 0 1 2 2v7"/><path d="M6 9v12"/></svg><span>Needs My Review</span><b>3</b></div>
        <div class="hive-app-demo__feed"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M10.268 21a2 2 0 0 0 3.464 0"/><path d="M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326"/></svg><span>Firing Alerts</span><b>2</b></div>
        <div class="hive-app-demo__feed hive-app-demo__feed--active"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="1"/></svg><span>Issues</span><b>22</b></div>
      </div>
      <div class="hive-app-demo__items">
        <div class="hive-app-demo__item hive-app-demo__item--selected">
          <strong>Dependency Dashboard</strong><em>10h</em>
          <span><b class="hive-app-demo__kind hive-app-demo__kind--issue">Issue</b>GitHub · hay-kot/hive-desktop #28</span>
        </div>
        <div class="hive-app-demo__item">
          <strong>Clicking an item should reopen the collapsed detail pane</strong><em>11h</em>
          <span><b class="hive-app-demo__kind hive-app-demo__kind--issue">Issue</b>GitHub · hay-kot/hive-desktop #26</span>
        </div>
        <div class="hive-app-demo__item">
          <strong>Feed list: group items by date with separators</strong><em>11h</em>
          <span><b class="hive-app-demo__kind hive-app-demo__kind--issue">Issue</b>GitHub · hay-kot/hive-desktop #23</span>
        </div>
        <div class="hive-app-demo__item">
          <strong>fix: reset env between spawns</strong><em>11h</em>
          <span><b class="hive-app-demo__kind hive-app-demo__kind--pr">Pull Request</b>GitHub · hay-kot/hive-desktop #24</span>
        </div>
      </div>
    </div>
  </div>
</section>

<section class="hive-strip">
  <div><strong>Watch everything</strong><span>GitHub, your Grafana alerts, and anything that can POST a webhook.</span></div>
  <div><strong>Route it through rules you own</strong><span>Rules your own coding agent wrote, in a config file that lives in your dotfiles.</span></div>
  <div><strong>Get one ping</strong><span>One local queue, one ping per real change.</span></div>
</section>

<section class="hive-metaharness-section">
  <div class="hive-metaharness-section__inner">
    <div class="hive-eyebrow">The routing engine</div>
    <h2>Sources in. Sorted feeds out.</h2>
    <p>The flow editor is the same canvas that runs in the app. Sources stream raw observations through filters and functions that pass, drop and split them, so a firehose of GitHub activity, firing alerts and webhook deliveries becomes a handful of feeds you can actually act on.</p>

    <div class="hive-layer-diagram" role="group" aria-label="Sources stream into flows, which route items into feeds, actions, and notifications">
      <div class="hive-layer hive-layer--sources">
        <strong>Sources</strong>
        <span>GitHub · Grafana · webhooks · what streams in</span>
      </div>
      <div class="hive-layer hive-layer--flows">
        <strong>Flows</strong>
        <span>filters · functions · your own JavaScript</span>
      </div>
      <div class="hive-session-row">
        <div class="hive-session-card">
          <strong>Feeds</strong>
          <span class="hive-session-item"><span class="hive-session-icon hive-session-icon--agent" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M4 11a9 9 0 0 1 9 9"/><path d="M4 4a16 16 0 0 1 16 16"/><circle cx="5" cy="19" r="1"/></svg></span>Needs My Review</span>
          <span class="hive-session-item"><span class="hive-session-icon hive-session-icon--terminal" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M4 11a9 9 0 0 1 9 9"/><path d="M4 4a16 16 0 0 1 16 16"/><circle cx="5" cy="19" r="1"/></svg></span>Firing Alerts</span>
        </div>
        <div class="hive-session-card">
          <strong>Actions</strong>
          <span class="hive-session-item"><span class="hive-session-icon hive-session-icon--agent" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/><path d="M20 3v4"/><path d="M22 5h-4"/></svg></span>Launch session</span>
          <span class="hive-session-item"><span class="hive-session-icon hive-session-icon--terminal" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M12 19h8M4 17l6-6-6-6"/></svg></span>Run shell command</span>
        </div>
        <div class="hive-session-card">
          <strong>Notifications</strong>
          <span class="hive-session-item"><span class="hive-session-icon hive-session-icon--agent" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M10.268 21a2 2 0 0 0 3.464 0"/><path d="M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326"/></svg></span>One ping per change</span>
          <span class="hive-session-item"><span class="hive-session-icon hive-session-icon--terminal" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/></svg></span>dedup · cooldown</span>
        </div>
      </div>
    </div>
  </div>
</section>

<section class="hive-preview-section">
  <div class="hive-eyebrow">The app</div>
  <h2>Where the work arrives.</h2>
  <p>Sources poll GitHub, Grafana and your own webhooks. Flows filter, split and route what comes back into feeds you arranged yourself, and every item carries the actions its flow defines. Triage stays on your machine and is never written back.</p>
  <p>Hive is built in three modes you switch between from the title bar. The inbox is the one you get on install; the other two are further behind it and ship switched off until they settle.</p>

  <div class="hive-preview-terminal" role="img" aria-label="The Hive Desktop inbox: a sidebar of feeds grouped by repository, and a list of GitHub issues and pull requests">
    <div class="hive-preview-terminal__nav" aria-hidden="true">
      <strong>Inbox</strong><span>|</span><span>Code</span><span>|</span><span>Agents</span>
      <em><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>Activity</em><b>⌘K</b>
    </div>
    <div class="hive-preview-terminal__body" aria-hidden="true">
      <div class="hive-preview-sidebar">
        <div class="hive-preview-sidebar__head"><strong>Hive</strong><span>GitHub · Grafana · 4 sources</span></div>
        <div class="hive-preview-sidebar__label">Feeds</div>
        <div class="hive-preview-sidebar__row"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M13 6h3a2 2 0 0 1 2 2v7"/><path d="M6 9v12"/></svg><span>Needs My Review</span><b>3</b></div>
        <div class="hive-preview-sidebar__row"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M10.268 21a2 2 0 0 0 3.464 0"/><path d="M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326"/></svg><span>Firing Alerts</span><b>2</b></div>
        <div class="hive-preview-sidebar__row"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="12" cy="12" r="4"/><path d="M16 8v5a3 3 0 0 0 6 0v-1a10 10 0 1 0-4 8"/></svg><span>Assigned</span><i>0</i></div>
        <div class="hive-preview-sidebar__row hive-preview-sidebar__row--folder"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg><span>hive-desktop</span><b>23</b></div>
        <div class="hive-preview-sidebar__row hive-preview-sidebar__row--child"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M13 6h3a2 2 0 0 1 2 2v7"/><path d="M6 9v12"/></svg><span>Open PRs</span><b>1</b></div>
        <div class="hive-preview-sidebar__row hive-preview-sidebar__row--child"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M11 21.73a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73z"/><path d="M12 22V12"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="m7.5 4.27 9 5.15"/></svg><span>Renovate PRs</span><i>0</i></div>
        <div class="hive-preview-sidebar__row hive-preview-sidebar__row--child hive-preview-sidebar__row--selected"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="1"/></svg><span>Issues</span><b>22</b></div>
        <div class="hive-preview-sidebar__row hive-preview-sidebar__row--folder"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"/></svg><span>colonyops/hive</span><b>2</b></div>
        <div class="hive-preview-sidebar__row hive-preview-sidebar__row--child"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M13 6h3a2 2 0 0 1 2 2v7"/><path d="M6 9v12"/></svg><span>Open PRs</span><i>0</i></div>
        <div class="hive-preview-sidebar__row hive-preview-sidebar__row--child"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M11 21.73a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73z"/><path d="M12 22V12"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="m7.5 4.27 9 5.15"/></svg><span>Renovate PRs</span><i>0</i></div>
        <div class="hive-preview-sidebar__row hive-preview-sidebar__row--child"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="1"/></svg><span>Issues</span><b>2</b></div>
        <div class="hive-preview-sidebar__space"></div>
        <div class="hive-preview-sidebar__row hive-preview-sidebar__row--muted"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M3 6h18"/><path d="M19 6v14c0 1-1 2-2 2H7c-1 0-2-1-2-2V6"/><path d="M8 6V4c0-1 1-2 2-2h4c1 0 2 1 2 2v2"/><path d="M10 11v6"/><path d="M14 11v6"/></svg><span>Trash</span><i>0</i></div>
        <div class="hive-preview-sidebar__footer"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><rect width="8" height="8" x="3" y="3" rx="2"/><path d="M7 11v4a2 2 0 0 0 2 2h4"/><rect width="8" height="8" x="13" y="13" rx="2"/></svg><span><strong>Edit flow</strong><em>Open editor</em></span></div>
      </div>
      <div class="hive-preview-pane">
        <div class="hive-preview-list__head">
          <span class="hive-preview-list__search"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>Search items, sources, people…</span>
          <span class="hive-preview-list__tabs"><b>All</b><span>Unread 0</span></span>
        </div>
        <div class="hive-preview-item hive-preview-item--selected">
          <span class="hive-preview-item__badge"><svg xmlns="http://www.w3.org/2000/svg" fill="currentColor" viewBox="0 0 16 16"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg></span>
          <div class="hive-preview-item__body">
            <div class="hive-preview-item__top"><strong>Dependency Dashboard</strong><em>10h</em></div>
            <div class="hive-preview-item__meta"><b class="hive-preview-kind hive-preview-kind--issue">Issue</b><span>GitHub · hay-kot/hive-desktop #28</span></div>
            <div class="hive-preview-item__snippet">renovate · This issue lists Renovate updates and detected dependencies.</div>
          </div>
        </div>
        <div class="hive-preview-item">
          <span class="hive-preview-item__badge"><svg xmlns="http://www.w3.org/2000/svg" fill="currentColor" viewBox="0 0 16 16"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg></span>
          <div class="hive-preview-item__body">
            <div class="hive-preview-item__top"><strong>Clicking an item should reopen the collapsed detail pane</strong><em>11h</em></div>
            <div class="hive-preview-item__meta"><b class="hive-preview-kind hive-preview-kind--issue">Issue</b><span>GitHub · hay-kot/hive-desktop #26</span></div>
            <div class="hive-preview-item__snippet">hay-kot · Summary</div>
          </div>
        </div>
        <div class="hive-preview-item">
          <span class="hive-preview-item__badge"><svg xmlns="http://www.w3.org/2000/svg" fill="currentColor" viewBox="0 0 16 16"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg></span>
          <div class="hive-preview-item__body">
            <div class="hive-preview-item__top"><strong>Feed list: group items by date with separators</strong><em>11h</em></div>
            <div class="hive-preview-item__meta"><b class="hive-preview-kind hive-preview-kind--issue">Issue</b><span>GitHub · hay-kot/hive-desktop #23</span></div>
            <div class="hive-preview-item__snippet">hay-kot · Summary</div>
          </div>
        </div>
        <div class="hive-preview-item">
          <span class="hive-preview-item__badge"><svg xmlns="http://www.w3.org/2000/svg" fill="currentColor" viewBox="0 0 16 16"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg></span>
          <div class="hive-preview-item__body">
            <div class="hive-preview-item__top"><strong>fix: reset env between spawns</strong><em>11h</em></div>
            <div class="hive-preview-item__meta"><b class="hive-preview-kind hive-preview-kind--pr">Pull Request</b><span>GitHub · hay-kot/hive-desktop #24</span></div>
            <div class="hive-preview-item__snippet">hay-kot · Resets the inherited environment before each spawn.</div>
          </div>
        </div>
        <div class="hive-preview-item">
          <span class="hive-preview-item__badge"><svg xmlns="http://www.w3.org/2000/svg" fill="currentColor" viewBox="0 0 16 16"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg></span>
          <div class="hive-preview-item__body">
            <div class="hive-preview-item__top"><strong>AI inbox: opt-in triage with user-provided credentials</strong><em>11h</em></div>
            <div class="hive-preview-item__meta"><b class="hive-preview-kind hive-preview-kind--issue">Issue</b><span>GitHub · hay-kot/hive-desktop #18</span></div>
            <div class="hive-preview-item__snippet">hay-kot · Summary</div>
          </div>
        </div>
        <div class="hive-preview-item">
          <span class="hive-preview-item__badge"><svg xmlns="http://www.w3.org/2000/svg" fill="currentColor" viewBox="0 0 16 16"><path d="M8 0C3.58 0 0 3.58 0 8c0 3.54 2.29 6.53 5.47 7.59.4.07.55-.17.55-.38 0-.19-.01-.82-.01-1.49-2.01.37-2.53-.49-2.69-.94-.09-.23-.48-.94-.82-1.13-.28-.15-.68-.52-.01-.53.63-.01 1.08.58 1.23.82.72 1.21 1.87.87 2.33.66.07-.52.28-.87.51-1.07-1.78-.2-3.64-.89-3.64-3.95 0-.87.31-1.59.82-2.15-.08-.2-.36-1.02.08-2.12 0 0 .67-.21 2.2.82.64-.18 1.32-.27 2-.27.68 0 1.36.09 2 .27 1.53-1.04 2.2-.82 2.2-.82.44 1.1.16 1.92.08 2.12.51.56.82 1.27.82 2.15 0 3.07-1.87 3.75-3.65 3.95.29.25.54.73.54 1.48 0 1.07-.01 1.93-.01 2.2 0 .21.15.46.55.38A8.01 8.01 0 0 0 16 8c0-4.42-3.58-8-8-8z"/></svg></span>
          <div class="hive-preview-item__body">
            <div class="hive-preview-item__top"><strong>Terminal mode: split panes</strong><em>12h</em></div>
            <div class="hive-preview-item__meta"><b class="hive-preview-kind hive-preview-kind--issue">Issue</b><span>GitHub · hay-kot/hive-desktop #31</span></div>
            <div class="hive-preview-item__snippet">hay-kot · Summary</div>
          </div>
        </div>
      </div>
    </div>
    <div class="hive-preview-terminal__footer" aria-hidden="true">j/k navigate · / search · ⌘K commands · ? shortcuts</div>
  </div>
</section>

<section class="hive-features-section">
  <div class="hive-section-heading">
    <h2>One queue, one ping.</h2>
    <p>Hive watches GitHub, Grafana and your webhooks, routes them through rules your coding agent wrote, and interrupts you exactly once per real change.</p>
  </div>

  <div class="hive-feature-grid">
    <a class="hive-feature-card" href="inbox/sources/">
      <span class="hive-feature-icon" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M22 12h-6l-2 3h-4l-2-3H2"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/></svg></span>
      <strong>Everything in, not just GitHub</strong>
      <p>Point a source at any GitHub search query or your notification inbox, across as many accounts as you have. Pair it with Grafana alerts, a PromQL expression, or anything that can POST JSON to a local endpoint. Read, archive and ignore state stays on your machine — triaging here never writes back to GitHub.</p>
      <small>github · grafana · webhooks · multi-account</small>
    </a>
    <a class="hive-feature-card" href="chats/agent-workspaces/">
      <span class="hive-feature-icon" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M9.937 15.5A2 2 0 0 0 8.5 14.063l-6.135-1.582a.5.5 0 0 1 0-.962L8.5 9.936A2 2 0 0 0 9.937 8.5l1.582-6.135a.5.5 0 0 1 .963 0L14.063 8.5A2 2 0 0 0 15.5 9.937l6.135 1.581a.5.5 0 0 1 0 .964L15.5 14.063a2 2 0 0 0-1.437 1.437l-1.582 6.135a.5.5 0 0 1-.963 0z"/><path d="M20 3v4"/><path d="M22 5h-4"/><path d="M4 17v2"/><path d="M5 18H3"/></svg></span>
      <strong>Your coding agent writes the rules</strong>
      <p>Hive renders skill files out of its own live node registry into ~/.claude, ~/.codex, ~/.pi and ~/.agents, then keeps them in step — add a node type and your agent already knows it. Describe the work you want surfaced and it writes the flow, dry-runs it against real input, and hands it back for you to review.</p>
      <small>generated skills · four agent directories · auto-synced</small>
    </a>
    <a class="hive-feature-card" href="inbox/how-it-works/">
      <span class="hive-feature-icon" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M10.268 21a2 2 0 0 0 3.464 0"/><path d="M3.262 15.326A1 1 0 0 0 4 17h16a1 1 0 0 0 .74-1.673C19.41 13.956 18 12.499 18 8A6 6 0 0 0 6 8c0 4.499-1.411 5.956-2.738 7.326"/></svg></span>
      <strong>One ping per real change</strong>
      <p>A notification means something actually changed. Re-polling the same PR never re-fires, a cooldown floors how often one item can reach you, pings that went stale while you were away are dropped instead of replayed as a burst, and a restart recomputes every feed without notifying anything twice.</p>
      <small>dedup · cooldown · staleness · replay-inert</small>
    </a>
    <a class="hive-feature-card" href="inbox/flows/">
      <span class="hive-feature-icon" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><rect width="8" height="8" x="3" y="3" rx="2"/><path d="M7 11v4a2 2 0 0 0 2 2h4"/><rect width="8" height="8" x="13" y="13" rx="2"/></svg></span>
      <strong>Rules that are programs, not filters</strong>
      <p>Wire sources through filters and functions on a canvas, watch per-node run status, then deploy. Filters are declarative and their reject branch is wireable, so nothing has to be thrown away. Functions are your own JavaScript with up to sixteen outputs and durable per-node memory — one alert can fan out into one tracked item per firing entity.</p>
      <small>nine node types · javascript · durable kv · fan-out</small>
    </a>
    <a class="hive-feature-card" href="configuration/settings/">
      <span class="hive-feature-icon" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M10 12.5 8 15l2 2.5"/><path d="m14 12.5 2 2.5-2 2.5"/><path d="M14 2v4a2 2 0 0 0 2 2h4"/><path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7z"/></svg></span>
      <strong>Config is a file you own</strong>
      <p>Flows and actions are plain YAML in your config directory: diff them, PR them, keep them in your dotfiles. Edit on the canvas or in your editor. Hive validates on save and reloads live, and a file that fails to build keeps its last good version in service rather than taking the feed down with it.</p>
      <small>flows/*.yaml · hot reload · last-good fallback</small>
    </a>
    <a class="hive-feature-card" href="inbox/actions/">
      <span class="hive-feature-icon" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M4 14a1 1 0 0 1-.78-1.63l9.9-10.2a.5.5 0 0 1 .86.46l-1.92 6.02A1 1 0 0 0 13 10h7a1 1 0 0 1 .78 1.63l-9.9 10.2a.5.5 0 0 1-.86-.46l1.92-6.02A1 1 0 0 0 11 14z"/></svg></span>
      <strong>Triage becomes action</strong>
      <p>An item can launch a coding-agent session from a prompt template, run a shell command, publish a message for another session to pick up, or render straight to your clipboard. An action can ask you for input first, and each one fires once per item. Run it by hand from the detail pane, or let a flow node do it for you.</p>
      <small>session · shell · message · clipboard</small>
    </a>
  </div>
</section>

## Start with Hive

<div class="hive-cta">
  <div>
    <h2>Install Hive and build your first feed.</h2>
    <p>Add a source, drop a filter on it, point it at a feed. Or paste our prompt into your agent and let it write the config.</p>
  </div>
  <div class="hive-cta__actions">
    <a class="md-button md-button--primary" href="getting-started/#install">Install</a>
    <a class="md-button" href="getting-started/">Getting started</a>
  </div>
</div>

---

<small>LLM-friendly: [llms.txt](llms.txt) | [llms-full.txt](llms-full.txt)</small>
