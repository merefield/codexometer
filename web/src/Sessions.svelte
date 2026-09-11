<script lang="ts">
  import { tick } from 'svelte';
  import { live, date, number, type Session } from './state.svelte';
  import {
    preferences,
    detailLevel,
    setDetailLevel,
  } from './preferences.svelte';
  import Graph from './Graph.svelte';
  let { params = {} }: { params?: { id?: string } } = $props();
  let sessions = $derived(live.data?.sessions || []);
  let selected = $derived(sessions.find((s) => s.id === params.id));
  let selectedID = $derived(
    sessions.some((s) => s.id === preferences.selected)
      ? preferences.selected
      : sessions[0]?.id,
  );
  let stale = $derived(!live.connected || !!live.data?.sessionsError);
  let attention = $derived(
    sessions.filter((s) =>
      ['INPUT NEEDED', 'APPROVAL NEEDED', 'CHECK SESSION'].includes(s.status),
    ),
  );

  function select(id: string) {
    preferences.selected = id;
    void tick().then(() =>
      document
        .querySelector('.session-row.selected')
        ?.scrollIntoView({ block: 'nearest' }),
    );
  }
  function change(id: string, delta: number) {
    select(id);
    if (params.id) {
      if (delta < 0) {
        setDetailLevel(id, 2);
        location.hash = '/sessions';
      }
      return;
    }
    const level = detailLevel(id) + delta;
    if (level > 2) location.hash = '/sessions/' + encodeURIComponent(id);
    else setDetailLevel(id, level);
  }
  function keydown(event: KeyboardEvent) {
    const target = event.target as HTMLElement;
    if (
      event.altKey ||
      event.ctrlKey ||
      event.metaKey ||
      event.shiftKey ||
      target.closest('input, textarea, select, [contenteditable="true"]')
    )
      return;
    if (event.key === 'Escape' && params.id) {
      event.preventDefault();
      change(params.id, -1);
    } else if (
      ['ArrowLeft', 'ArrowRight'].includes(event.key) &&
      (params.id || selectedID)
    ) {
      event.preventDefault();
      change(params.id || selectedID!, event.key === 'ArrowLeft' ? -1 : 1);
    } else if (
      !params.id &&
      ['ArrowUp', 'ArrowDown'].includes(event.key) &&
      sessions.length
    ) {
      event.preventDefault();
      const index = sessions.findIndex((s) => s.id === selectedID);
      select(
        sessions[
          Math.max(
            0,
            Math.min(
              sessions.length - 1,
              index + (event.key === 'ArrowUp' ? -1 : 1),
            ),
          )
        ].id,
      );
    }
  }
  function explanation(session: Session) {
    if (stale)
      return 'Last observation only — refresh unavailable. Check Codex for current state.';
    if (session.status === 'CHECK SESSION')
      return 'INFERRED INACTIVITY — a quiet session, not a confirmed input or approval request. Local tools may still be running.';
    if (session.status === 'APPROVAL NEEDED')
      return 'OBSERVED APPROVAL SIGNAL — approve or decline in Codex.';
    if (session.status === 'INPUT NEEDED')
      return 'OBSERVED INPUT SIGNAL — reply in Codex.';
    if (session.status === 'TURN COMPLETE')
      return 'OBSERVED TURN COMPLETION — informational, not an approval request.';
    return '';
  }
</script>

<svelte:window onkeydown={keydown} />

{#snippet context(session: Session, heading = true)}
  {#if explanation(session)}<p
      class="attention-note"
      class:inferred={session.status === 'CHECK SESSION'}
    >
      {explanation(session)}
    </p>{/if}
  {#if heading}<h3>{session.contextKind || 'LAST ACTIVITY'}</h3>{/if}
  <pre>{session.text || 'No session context available.'}</pre>
  {#if session.command || session.status === 'APPROVAL NEEDED'}
    <hr />
    <h3>
      {stale || session.status !== 'APPROVAL NEEDED'
        ? 'LAST OBSERVED COMMAND'
        : 'COMMAND TO APPROVE IN CODEX'}
    </h3>
    {#if session.command}<pre class="command">{session.command}</pre>{:else}<p
        class="notice"
      >
        Command unavailable from this observation. Open Codex to inspect the
        request.
      </p>{/if}
  {/if}
  <p class="muted">
    CONTEXT SOURCE // {session.source || 'LOCAL'} // READ ONLY
  </p>
{/snippet}

<div class="spread">
  <h1>SESSION TOTALS</h1>
  <span class="eyebrow">OBSERVED {date(live.data?.sessionsAt)}</span>
</div>
<p class="muted">
  Local session tokens observed since this server started; linked agents are
  grouped with their parent. History samples every 30 seconds. These are not
  account-wide totals.
</p>
{#if stale}<p class="notice">
    Session connection or refresh unavailable. Context and telemetry may be
    stale.
  </p>{/if}
{#if attention.length && !stale}<nav
    class="attention-summary"
    aria-label="Sessions needing attention"
  >
    {#each attention as session}<a
        class="button"
        href={'#/sessions/' + encodeURIComponent(session.id)}
        onclick={() => select(session.id)}
        >{session.status} // {session.directory || session.id}</a
      >{/each}
  </nav>{/if}
{#if params.id}
  <a
    class="button"
    href="#/sessions"
    onclick={() => {
      select(params.id!);
      setDetailLevel(params.id!, 2);
    }}>← ALL SESSIONS</a
  >
  {#if selected}<section class="panel full-detail">
      <h2>
        <span
          class="lamp lit"
          class:working={selected.status === 'WORKING' && !stale}
        ></span>{stale ? 'STALE' : selected.status} // {selected.directory}
      </h2>
      <p class="muted">{number(selected.tokens)} TOKENS // {selected.id}</p>
      {@render context(selected)}
      <p class="notice">Read only — reply or approve in Codex.</p>
    </section>{:else}<p class="empty">
      This session is no longer in the current observation. <a href="#/sessions"
        >Return to sessions</a
      >.
    </p>{/if}
{:else}
  <div class="spread session-controls">
    <p class="muted">
      ↑ ↓ SELECT SESSION // ← LESS DETAIL // → MORE DETAIL // ESC BACK
    </p>
    <div>
      <button onclick={() => sessions.forEach((s) => setDetailLevel(s.id, 1))}
        >SHOW ALL DETAILS</button
      >
      <button onclick={() => sessions.forEach((s) => setDetailLevel(s.id, 0))}
        >HIDE ALL DETAILS</button
      >
    </div>
  </div>
  {#each sessions as session (session.id)}
    {@const level = detailLevel(session.id)}
    <section
      class="session-row"
      class:selected={selectedID === session.id}
      class:wide={level === 2}
      class:split={level === 1}
      aria-label={'Session ' + (session.directory || session.id)}
    >
      <div class="panel telemetry">
        <h2>
          <span
            class="lamp lit"
            class:working={session.status === 'WORKING' && !stale}
          ></span>{stale ? 'STALE' : session.status}
        </h2>
        <button
          class="session-select"
          aria-pressed={selectedID === session.id}
          onclick={() => select(session.id)}
          >{session.directory || session.id}</button
        >
        <p class="readout">{number(session.tokens)} <small>TOKENS</small></p>
        <p>{session.agents} LINKED AGENTS</p>
        <p class="muted">ACTIVE // {date(session.activity)}</p>
        <div class="detail-controls">
          <button
            aria-label="Less detail"
            disabled={level === 0}
            onclick={() => change(session.id, -1)}>←</button
          >
          <button
            onclick={() => {
              select(session.id);
              setDetailLevel(session.id, level ? 0 : 1);
            }}
            aria-expanded={level > 0}
            >{level ? 'HIDE DETAIL' : 'SHOW DETAIL'}</button
          >
          <button aria-label="More detail" onclick={() => change(session.id, 1)}
            >→</button
          >
        </div>
        <a
          class="button"
          href={'#/sessions/' + encodeURIComponent(session.id)}
          onclick={() => select(session.id)}>FULL DETAIL →</a
        >
        {#if !stale && ['APPROVAL NEEDED', 'INPUT NEEDED', 'CHECK SESSION'].includes(session.status)}<p
            class="attention-note"
          >
            {session.status === 'CHECK SESSION'
              ? 'INFERRED INACTIVITY'
              : 'REPLY IN CODEX'}
          </p>{/if}
      </div>
      {#if level > 0}<div class="panel context">
          <div class="spread">
            <h2>{session.contextKind || 'LAST ACTIVITY'}</h2>
            {#if !stale && session.status === 'APPROVAL NEEDED'}<a
                class="attention-badge"
                href={'#/sessions/' + encodeURIComponent(session.id)}
                >APPROVAL IN CODEX ↗</a
              >{/if}
          </div>
          {@render context(session, false)}
        </div>{/if}
      {#if level < 2}<div class="panel graph-panel">
          <h2>TOKEN ACTIVITY // 30 SECOND SAMPLES</h2>
          <Graph
            values={(session.samples || []).map((s) => s.tokens)}
            capacity={120}
          />
          <p class="muted">
            LAST {session.samples?.length || 0} SAMPLES // AUTO SCALE
          </p>
        </div>{/if}
    </section>
  {/each}
  {#if !sessions.length}<p class="empty">
      No locally observed sessions yet. Keep Codex running alongside
      Codexometer.
    </p>{/if}
{/if}
