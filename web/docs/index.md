---
icon: fontawesome/brands/hive
hide:
  - toc
---

<section class="hive-hero">
  <div class="hive-eyebrow">Local-first · Open source</div>
  <h1>Collect work, route it, and act.</h1>
  <p class="hive-lede">Hive Desktop pulls pull requests, issues, alerts, and your own data into feeds you design, then hands any item to a coding agent. The same app runs those agents in tmux and hosts chat workspaces for work outside a repository.</p>
  <div class="hive-hero__actions">
    <a class="md-button md-button--primary" data-hive-download href="getting-started/#install">Download</a>
    <a class="md-button" href="getting-started/">Read the docs</a>
  </div>
  <p class="hive-hero__meta" data-hive-download-meta><a href="getting-started/#install">All downloads</a></p>
  <div class="hive-install">
    <pre><code>curl -fsSL https://hivedesktop.com/install.sh | bash</code></pre>
  </div>
  <p class="hive-hero__note">MIT licensed · macOS and Linux</p>
</section>

<section class="hive-strip">
  <a href="#feeds"><span class="hive-strip__icon" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M22 12h-6l-2 3h-4l-2-3H2"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/></svg></span><strong>Feeds</strong><span class="hive-strip__desc">Gather scattered work into feeds you design, evaluate it once, and decide what happens next.</span></a>
  <a href="#code"><span class="hive-strip__icon" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="m7 11 2-2-2-2"/><path d="M11 13h4"/><rect width="18" height="18" x="3" y="3" rx="2" ry="2"/></svg></span><strong>Code</strong><span class="hive-strip__desc">Run several streams of agent work at once on the hive CLI's tmux session engine.</span></a>
  <a href="#chats"><span class="hive-strip__icon" aria-hidden="true"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M12 8V4H8"/><rect width="16" height="12" x="4" y="8" rx="2"/><path d="M2 14h2"/><path d="M20 14h2"/><path d="M15 13v2"/><path d="M9 13v2"/></svg></span><strong>Chats</strong><span class="hive-strip__desc">Give an agent a workspace with skills and MCP servers for work that is not code.</span></a>
</section>

<section class="hive-showcase" id="feeds">
  <div class="hive-showcase__copy">
    <div class="hive-eyebrow"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M22 12h-6l-2 3h-4l-2-3H2"/><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"/></svg>Feeds</div>
    <h2>Bring scattered work into feeds you design.</h2>
    <p>Hive collects from GitHub, Gitea, Grafana, PostHog, local webhooks, and any command that prints JSON. Flows route every item into the feed where you will look at it, so you evaluate once and decide what to do.</p>
    <ul>
      <li><strong>Any data, any feed.</strong> Filters cover the common rules. JavaScript function nodes parse and reshape any payload, split it across outputs, and route it into whatever feed structure you want.</li>
      <li><strong>One ping per real change.</strong> Notify nodes deduplicate and cool down, so a firing alert or a moving pull request interrupts you once.</li>
      <li><strong>From item to session.</strong> A launch-session action starts a coding agent from an item's detail pane, with the repository checked out and the item as the prompt.</li>
    </ul>
    <a class="hive-showcase__link" href="inbox/flows/">How flows work</a>
  </div>
  <figure class="hive-demo">
    <div class="hive-demo__bar" aria-hidden="true"><span></span><span></span><span></span><strong>Feeds</strong></div>
    <video class="hive-demo__video" controls playsinline preload="metadata" aria-label="Demo: working through feeds in Hive Desktop">
      <source src="assets/demos/feeds.mp4" type="video/mp4">
    </video>
    <div class="hive-demo__placeholder" aria-hidden="true">Demo video coming soon</div>
  </figure>
</section>

<section class="hive-showcase hive-showcase--flip" id="code">
  <div class="hive-showcase__copy">
    <div class="hive-eyebrow"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="m7 11 2-2-2-2"/><path d="M11 13h4"/><rect width="18" height="18" x="3" y="3" rx="2" ry="2"/></svg>Code</div>
    <h2>Run several streams of work at once.</h2>
    <p>Code is built on the hive CLI's session engine and tmux. Each session gets its own checkout and windows, keeps running when Hive closes, and stays visible to an existing hive install and to <code>tmux attach</code>.</p>
    <ul>
      <li><strong>A sidebar for every session.</strong> Attach to any window, switch with <kbd>⌘1</kbd> through <kbd>⌘9</kbd>, filter sessions, and search scrollback.</li>
      <li><strong>Your commands in the menus.</strong> Actions add commands to session and window menus. Quick terminals open tools such as lazygit in the active checkout.</li>
      <li><strong>Scratch terminals.</strong> A tmux session for shells that belong to no repository, with tabs you can rename and reorder.</li>
    </ul>
    <a class="hive-showcase__link" href="code/terminal-mode/">Terminal mode</a>
  </div>
  <figure class="hive-demo">
    <div class="hive-demo__bar" aria-hidden="true"><span></span><span></span><span></span><strong>Code</strong></div>
    <video class="hive-demo__video" controls playsinline preload="metadata" aria-label="Demo: running coding agent sessions in Hive Desktop">
      <source src="assets/demos/code.mp4" type="video/mp4">
    </video>
    <div class="hive-demo__placeholder" aria-hidden="true">Demo video coming soon</div>
  </figure>
</section>

<section class="hive-showcase" id="chats">
  <div class="hive-showcase__copy">
    <div class="hive-eyebrow"><svg xmlns="http://www.w3.org/2000/svg" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="2" viewBox="0 0 24 24"><path d="M12 8V4H8"/><rect width="16" height="12" x="4" y="8" rx="2"/><path d="M2 14h2"/><path d="M20 14h2"/><path d="M15 13v2"/><path d="M9 13v2"/></svg>Chats</div>
    <h2>Capture context for work that is not code.</h2>
    <p>A chat is a named workspace with an agent, skill packages, and MCP servers. Point one at Slack threads, call transcripts, and email to learn how people feel about your product and what to build next. Give another OpenSCAD skills and a render script that writes G-code, and prototype a 3D print from a prompt.</p>
    <ul>
      <li><strong>Built to your spec.</strong> Each workspace chooses Claude Code or Codex, an approval mode, skills, and MCP servers, and keeps its instructions in an <code>AGENTS.md</code> file.</li>
      <li><strong>The Hive workspace.</strong> Hive ships a workspace that knows its own configuration. Ask it for a feed, an action, a shortcut, or a setting, and review the file it writes.</li>
      <li><strong>An MCP server for the app.</strong> Any agent can connect to Hive's local MCP server to read feeds, refresh sources, and dry-run a flow against a payload.</li>
    </ul>
    <a class="hive-showcase__link" href="chats/agent-workspaces/">Agent workspaces</a>
  </div>
  <figure class="hive-demo">
    <div class="hive-demo__bar" aria-hidden="true"><span></span><span></span><span></span><strong>Chats</strong></div>
    <video class="hive-demo__video" controls playsinline preload="metadata" aria-label="Demo: agent workspaces in Hive Desktop">
      <source src="assets/demos/chats.mp4" type="video/mp4">
    </video>
    <div class="hive-demo__placeholder" aria-hidden="true">Demo video coming soon</div>
  </figure>
</section>

<div class="hive-cta">
  <div>
    <h2>Install Hive and build your first feed.</h2>
    <p>Add a source, route it to a feed, and choose when Hive should notify or act.</p>
  </div>
  <div class="hive-cta__actions">
    <a class="md-button md-button--primary" data-hive-download href="getting-started/#install">Download</a>
    <a class="md-button" href="getting-started/">Getting Started</a>
  </div>
</div>

---

<small>LLM-friendly: [llms.txt](llms.txt) | [llms-full.txt](llms-full.txt)</small>
