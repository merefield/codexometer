<script lang="ts">
  import { tick, untrack } from 'svelte';
  import { router } from 'svelte-spa-router';
  import { live, date, number, type Session } from './state.svelte';
  import {
    preferences,
    detailLevel,
    setDetailLevel,
    setAllDetailLevels,
  } from './preferences.svelte';
  import Graph from './Graph.svelte';
  import SessionActions from './SessionActions.svelte';
  import SessionCopy from './SessionCopy.svelte';
  let { params = {} }: { params?: { id?: string } } = $props();
  let sessions = $derived(live.data?.sessions || []);
  let profiles = $derived(live.data?.control ? live.data.profiles || [] : []);
  let nativeProtected = $state(false);
  function openProfile(event: MouseEvent, id: string) {
    if (params.id && params.id !== id && nativeProtected) {
      event.preventDefault();
      return;
    }
    select(id);
  }
  // Every entry route (links, arrows, deep links and browser Forward) leaves a
  // wide row behind, so native browser Back agrees with Escape/All Sessions.
  $effect(() => {
    const id = params.id;
    if (id)
      untrack(() => {
        preferences.selected = id;
        setDetailLevel(id, 2);
      });
  });
  let selected = $derived(sessions.find((s) => s.id === params.id));
  let profileFocused = $derived(
    new URLSearchParams(router.querystring).get('review') === 'profile' &&
      profiles.some((p) => p.session === params.id && p.pending),
  );
  let selectedID = $derived(
    sessions.some((s) => s.id === preferences.selected)
      ? preferences.selected
      : sessions[0]?.id,
  );
  let stale = $derived(!live.connected || !!live.data?.sessionsError);
  // Parent rows already include linked-agent usage. Sum each listed row once;
  // do not add agent counts or activity samples to its observed token counter.
  let totals = $derived([
    ['OBSERVED TOKENS', number(sessions.reduce((sum, s) => sum + s.tokens, 0))],
    ['LISTED SESSIONS', number(sessions.length)],
    ...[
      ['WORKING', 'WORKING'],
      ['AWAITING APPROVAL', 'APPROVAL NEEDED'],
      ['AWAITING INPUT', 'INPUT NEEDED'],
      ['CHECK · INFERRED', 'CHECK SESSION'],
    ].map(([label, status]) => [
      label,
      stale ? '—' : number(sessions.filter((s) => s.status === status).length),
    ]),
  ]);
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
    if (
      live.data?.control &&
      ['APPROVAL NEEDED', 'INPUT NEEDED'].includes(session.status)
    )
      return 'Open full detail for supported live controls; otherwise reply in Codex.';
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

{#snippet context(session: Session, heading = true, full = false)}
  {#if explanation(session) && (!full || stale || session.status === 'CHECK SESSION')}<p
      class="attention-note"
      class:inferred={session.status === 'CHECK SESSION'}
    >
      {explanation(session)}
    </p>{/if}
  {#if heading}<h3>{session.contextKind || 'LAST ACTIVITY'}</h3>{/if}
  <pre>{session.text || 'No session context available.'}</pre>
  {#if (!full || !live.data?.control) && (session.command || session.status === 'APPROVAL NEEDED')}
    <hr />
    <h3>
      {stale || session.status !== 'APPROVAL NEEDED'
        ? 'LAST OBSERVED COMMAND'
        : live.data?.control
          ? 'COMMAND REQUEST'
          : 'COMMAND TO APPROVE IN CODEX'}
    </h3>
    {#if session.command}<pre class="command">{session.command}</pre>{:else}<p
        class="notice"
      >
        Command unavailable from this observation. Open Codex to inspect the
        request.
      </p>{/if}
  {/if}
  {#if !full}<p class="muted">
      CONTEXT SOURCE // {session.source || 'LOCAL'} // OBSERVATION
    </p>{/if}
{/snippet}

<div class="spread">
  <h1>SESSION TOTALS</h1>
  <span class="eyebrow">OBSERVED {date(live.data?.sessionsAt)}</span>
</div>
<dl class="session-totals" aria-label="Session totals" class:stale>
  {#each totals as [label, value]}<div>
      <dt>{label}</dt>
      <dd>{value}</dd>
    </div>{/each}
</dl>
<p class="muted">
  {stale ? 'LAST KNOWN TOTALS — live state counts unavailable. ' : ''}Tokens
  observed since this server started for currently listed sessions; linked
  agents are already included. Totals can decrease when a session leaves the
  list. History samples every 30 seconds. Not account-wide totals.
</p>
{#if stale}<p class="notice">
    Session connection or refresh unavailable. Context and telemetry may be
    stale.
  </p>{/if}
{#if live.data?.profileError}<p class="notice">
    Some quota profile checks are unavailable. Only sessions with freshly
    verified quota and settings can be updated; previous outcome notices remain
    visible.
  </p>{/if}
{#if (attention.length || profiles.some((p) => p.pending)) && !stale}<nav
    class="attention-summary"
    aria-label="Sessions needing attention"
  >
    {#each attention as session}<a
        class="button"
        class:approval={session.status === 'APPROVAL NEEDED'}
        href={'#/sessions/' + encodeURIComponent(session.id)}
        onclick={() => select(session.id)}
        >{session.status} // {session.directory || session.id}</a
      >{/each}
    {#each profiles.filter((p) => p.pending && sessions.some((s) => s.id === p.session)) as profile}
      <a
        class="button approval"
        title={params.id && params.id !== profile.session && nativeProtected
          ? 'Finish sending or clear your current draft before switching sessions.'
          : 'Review this session’s quota threshold'}
        href={'#/sessions/' +
          encodeURIComponent(profile.session) +
          '?review=profile'}
        onclick={(event) => openProfile(event, profile.session)}
        >QUOTA THRESHOLD // {sessions.find((s) => s.id === profile.session)
          ?.directory || profile.session}</a
      >
    {/each}
  </nav>{/if}
{#if params.id}
  {#if selected}<section class="panel full-detail">
      <div class="detail-heading">
        <h2>
          <span
            class="lamp lit"
            class:working={selected.status === 'WORKING' && !stale}
          ></span>{stale
            ? 'STALE'
            : profileFocused
              ? 'QUOTA THRESHOLD'
              : selected.status} // {selected.name || selected.directory}
        </h2>
        <a
          class="button"
          href="#/sessions"
          onclick={() => {
            select(params.id!);
            setDetailLevel(params.id!, 2);
          }}>← ALL SESSIONS</a
        >
      </div>
      {#if selected.name && selected.directory}<p class="muted">
          {selected.directory}
        </p>{/if}
      <p class="muted detail-metadata">
        {number(selected.tokens)} TOKENS // {selected.id} // CONTEXT SOURCE // {selected.source ||
          'LOCAL'}
      </p>
      <div
        class="detail-workspace"
        style:display={profileFocused ? 'none' : undefined}
      >
        <div class="detail-context">
          {@render context(selected, true, true)}
        </div>
        {#if live.data?.control}
          {#key selected.id}<SessionActions
              session={selected.id}
              observedCommand={selected.command}
              suspended={profileFocused}
              onProtectedChange={(value) => {
                nativeProtected = value;
              }}
            />{/key}
        {:else}<p class="notice">Read only — reply or approve in Codex.</p>{/if}
      </div>
      {#each profiles.filter((p) => p.session === selected.id) as profile (profile.session)}
        <section id={'quota-profile-' + selected.id} class="profile-review">
          {#if profile.notice}<p class="notice" role="status">
              {profile.notice}
            </p>{/if}
          {#if profile.pending && profileFocused}<SessionActions
              session={selected.id}
              review="profile"
            />{:else if profile.pending}<a
              class="button approval"
              href={'#/sessions/' +
                encodeURIComponent(selected.id) +
                '?review=profile'}>QUOTA THRESHOLD // REVIEW PROFILE ↗</a
            >{/if}
        </section>
      {/each}
      {#if !profileFocused}{#key selected.id}<SessionCopy
            session={selected}
            active
          />{/key}{/if}
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
      <button onclick={() => setAllDetailLevels(1)}>SHOW ALL DETAILS</button>
      <button onclick={() => setAllDetailLevels(0)}>HIDE ALL DETAILS</button>
    </div>
  </div>
  {#each sessions as session (session.id)}
    {@const level = detailLevel(session.id)}
    <section
      class="session-row"
      class:selected={selectedID === session.id}
      class:wide={level === 2}
      class:split={level === 1}
      aria-label={'Session ' +
        (session.name || session.directory || session.id)}
    >
      <div class="panel telemetry">
        {#if session.name}
          <h2>
            <button
              class="session-select"
              aria-pressed={selectedID === session.id}
              onclick={() => select(session.id)}
              >SESSION // {session.name}</button
            >
          </h2>
        {/if}
        <h2>
          <span
            class="lamp lit"
            class:working={session.status === 'WORKING' && !stale}
          ></span>{stale ? 'STALE' : session.status}
        </h2>
        {#if !session.name}<button
            class="session-select"
            aria-pressed={selectedID === session.id}
            onclick={() => select(session.id)}
            >{session.directory || session.id}</button
          >{/if}
        <p class="readout">{number(session.tokens)} <small>TOKENS</small></p>
        {#if session.name && session.directory}<p class="muted">
            {session.directory}
          </p>{/if}
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
              : live.data?.control
                ? 'OPEN FULL DETAIL OR REPLY IN CODEX'
                : session.status === 'APPROVAL NEEDED'
                  ? 'APPROVE OR DECLINE IN CODEX'
                  : 'REPLY IN CODEX'}
          </p>{/if}
      </div>
      {#if level > 0}<div class="panel context">
          <div class="spread">
            <h2>{session.contextKind || 'LAST ACTIVITY'}</h2>
            {#if !stale && session.status === 'APPROVAL NEEDED'}<a
                class="attention-badge"
                href={'#/sessions/' + encodeURIComponent(session.id)}
                >{live.data?.control
                  ? 'REVIEW IN FULL DETAIL ↗'
                  : 'APPROVAL IN CODEX ↗'}</a
              >{/if}
          </div>
          {@render context(session, false)}
          {#each profiles.filter((p) => p.session === session.id) as profile}
            {#if profile.notice}<p class="notice">{profile.notice}</p>{/if}
            {#if profile.pending}<a
                class="attention-badge"
                href={'#/sessions/' +
                  encodeURIComponent(session.id) +
                  '?review=profile'}
                onclick={(event) => openProfile(event, session.id)}
                >QUOTA THRESHOLD // REVIEW PROFILE ↗</a
              >{/if}
          {/each}
          <SessionCopy {session} active={selectedID === session.id} />
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
