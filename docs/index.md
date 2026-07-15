---
layout: home
title: PBVex documentation
titleTemplate: false
description: Convex-shaped TypeScript authoring and clients, deployed to one PocketBase-based Go binary.
---

<main class="pbvex-home">
  <section class="pbvex-hero" aria-labelledby="pbvex-home-title">
    <div class="pbvex-hero-copy">
      <p class="pbvex-eyebrow">Product documentation</p>
      <h1 id="pbvex-home-title">PBVex</h1>
      <h2 class="pbvex-hero-title">TypeScript functions. One PocketBase-based Go binary.</h2>
      <p class="pbvex-lede">Convex-shaped TypeScript authoring and typed clients, backed by a single PocketBase-based backend you can run and operate yourself.</p>
      <div class="pbvex-actions">
        <a class="pbvex-button pbvex-button-primary" href="./quickstart">Start the quickstart <span aria-hidden="true">→</span></a>
        <a class="pbvex-button" href="./getting-started/agent-skills">Install agent skills</a>
        <a class="pbvex-button" href="./concepts/how-it-works">How it works</a>
      </div>
    </div>
    <div class="pbvex-terminal" aria-label="PBVex installation and client example">
      <div class="pbvex-terminal-bar"><span></span><span></span><span></span><code>terminal</code></div>
      <div class="pbvex-terminal-code">$ npm install --save-dev pbvex<br>$ npm install @pbvex/client<br>$ npx pbvex init<br>$ npm run pbvex:dev<br><br>// app.ts — generated references keep calls typed<br>import { Client } from '@pbvex/client';<br>import { api } from './pbvex/_generated/api.js';<br><br>const client = new Client('http://127.0.0.1:8090');<br>const messages = await client.query(api.messages.list, { channel: 'general' });<br>await client.mutation(api.messages.send, { channel: 'general', body: 'Hello' });</div>
    </div>
  </section>

  <section class="pbvex-paths" aria-labelledby="choose-path">
    <div>
      <p class="pbvex-eyebrow">Choose a path</p>
      <h2 id="choose-path">Build, connect, or run PBVex</h2>
    </div>
    <nav class="pbvex-link-grid" aria-label="Documentation paths">
      <a href="./quickstart"><strong>Quickstart</strong><span>Set up a backend and deploy an app.</span></a>
      <a href="./guides/"><strong>Guides</strong><span>Build, connect, deploy, and operate an application.</span></a>
      <a href="./guides/functions"><strong>Backend functions</strong><span>Queries, mutations, actions, and validators.</span></a>
      <a href="./guides/data-types-and-validation"><strong>Data and validation</strong><span>Every value type, validator, and runtime boundary.</span></a>
      <a href="./guides/client/"><strong>Client SDK</strong><span>Typed calls, authentication, and realtime.</span></a>
      <a href="./guides/react/"><strong>React</strong><span>Provider, hooks, and subscriptions.</span></a>
      <a href="./guides/svelte/"><strong>Svelte</strong><span>Svelte 5 runes, context, and reactive query state.</span></a>
      <a href="./getting-started/agent-skills"><strong>Agent skills</strong><span>Give coding agents PBVex-specific workflows and boundaries.</span></a>
      <a href="./self-hosting"><strong>Self-hosting</strong><span>Install one application, manage its data, and access the admin dashboard.</span></a>
      <a href="./api-reference/"><strong>API reference</strong><span>Generated TypeScript and Go reference.</span></a>
    </nav>
  </section>

  <section class="pbvex-runtime" aria-labelledby="skills-title">
    <div>
      <p class="pbvex-eyebrow">Agent-ready workflow</p>
      <h2 id="skills-title">Give your coding agent the PBVex contract.</h2>
      <p>Install the umbrella skill for end-to-end work, or select focused skills for backend, functions, clients, frameworks, components, and operations.</p>
      <pre><code>npx skills add nathabonfim59/pbvex --skill pbvex</code></pre>
    </div>
    <aside class="pbvex-limit" aria-labelledby="skills-detail-title">
      <h3 id="skills-detail-title">Install only what the task needs</h3>
      <p>The suite uses progressive disclosure, keeping framework and backend instructions out of context until they are relevant.</p>
      <a href="./getting-started/agent-skills">Browse all PBVex skills <span aria-hidden="true">→</span></a>
    </aside>
  </section>

  <section class="pbvex-runtime" aria-labelledby="runtime-title">
    <div>
      <p class="pbvex-eyebrow">Deploy one process</p>
      <h2 id="runtime-title">The backend is one executable.</h2>
      <p>It embeds PocketBase, its admin UI, the PBVex runtime, database bridge, realtime subscriptions, scheduler, storage, and authentication. At runtime it needs no Node.js, pnpm, repository checkout, or sidecar.</p>
      <pre><code>cd backend
go build -o pbvex ./cmd/pbvex
./pbvex serve --http 127.0.0.1:8090</code></pre>
    </div>
    <aside class="pbvex-limit" aria-labelledby="limits-title">
      <h3 id="limits-title">Know the operating model</h3>
      <p>PBVex v1 is designed for exactly one backend process and one data directory. It does not provide multi-node consensus or distributed scheduler coordination.</p>
      <a href="./guides/limits">Read runtime limits <span aria-hidden="true">→</span></a>
    </aside>
  </section>

  <section class="pbvex-next" aria-labelledby="next-title">
    <p class="pbvex-eyebrow">Continue with confidence</p>
    <h2 id="next-title">From first function to production operation</h2>
    <p><a href="./guides/">Browse all guides</a> · <a href="./guides/deployment">Deploy an application</a> · <a href="./guides/going-to-production">Go to production</a></p>
  </section>
</main>
