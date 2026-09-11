<script lang="ts">
  import { live, date, number } from './state.svelte';
  import Graph from './Graph.svelte';
  let { params = {} }: { params?: { id?: string } } = $props();
  let expanded = $state<Record<string, boolean>>({});
  let selected = $derived(live.data?.sessions.find((s) => s.id === params.id));
</script>

<div class="spread">
  <h1>SESSION TOTALS</h1>
  <span class="eyebrow">OBSERVED {date(live.data?.sessionsAt)}</span>
</div>
<p class="muted">
  Local session tokens observed since this server started; linked agents are
  grouped with their parent. History samples every 30 seconds. These are not
  account-wide totals.
</p>
{#if live.data?.sessionsError}<p class="notice">
    Session refresh failed. Context and telemetry may be stale.
  </p>{/if}
{#if params.id}
  <a class="button" href="#/sessions">← ALL SESSIONS</a>
  {#if selected}
    <section class="panel full-detail">
      <h2>
        {!live.connected ? 'STALE' : selected.status} // {selected.directory}
      </h2>
      <p class="muted">
        {selected.source || 'LOCAL'} // {number(selected.tokens)} TOKENS // {selected.id}
      </p>
      <h3>{selected.contextKind}</h3>
      <pre>{selected.text || 'No session context available.'}</pre>
      {#if selected.command}<hr />
        <h3>COMMAND TO APPROVE IN CODEX</h3>
        <pre class="command">{selected.command}</pre>{/if}
      <p class="notice">Read only — reply or approve in Codex.</p>
    </section>
  {:else}<p class="empty">
      This session is no longer in the current observation. <a href="#/sessions"
        >Return to sessions</a
      >.
    </p>{/if}
{:else}
  {#each live.data?.sessions || [] as session (session.id)}
    <section class="session-row">
      <div class="panel telemetry">
        <h2>
          <span
            class:working={session.status === 'WORKING' && live.connected}
            class="lamp lit"
          ></span>{!live.connected ? 'STALE' : session.status}
        </h2>
        <h3>{session.directory || session.id}</h3>
        <p class="readout">{number(session.tokens)} <small>TOKENS</small></p>
        <p>{session.agents} LINKED AGENTS</p>
        <p class="muted">ACTIVE // {date(session.activity)}</p>
        <button
          onclick={() => (expanded[session.id] = !expanded[session.id])}
          aria-expanded={!!expanded[session.id]}
          >{expanded[session.id] ? 'HIDE DETAIL' : 'SHOW DETAIL'}</button
        >
        <a class="button" href={'#/sessions/' + encodeURIComponent(session.id)}
          >FULL DETAIL →</a
        >
      </div>
      {#if expanded[session.id]}<div class="panel context">
          <h2>{session.contextKind}</h2>
          <pre>{session.text || 'No context available.'}</pre>
          {#if session.command}<hr />
            <h3>COMMAND TO APPROVE IN CODEX</h3>
            <pre class="command">{session.command}</pre>{/if}
          <p class="muted">{session.source || 'LOCAL'} // READ ONLY</p>
        </div>{/if}
      <div class="panel graph-panel">
        <h2>TOKEN ACTIVITY // 30 SECOND SAMPLES</h2>
        <Graph
          values={(session.samples || []).map((s) => s.tokens)}
          capacity={120}
        />
        <p class="muted">
          LAST {session.samples?.length || 0} SAMPLES // AUTO SCALE
        </p>
      </div>
    </section>
  {/each}
  {#if !live.data?.sessions.length}<p class="empty">
      No locally observed sessions yet. Keep Codex running alongside
      Codexometer.
    </p>{/if}
{/if}
