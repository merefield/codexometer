<script lang="ts">
  import { onMount, tick, untrack } from 'svelte';
  import { router } from 'svelte-spa-router';
  import {
    live,
    date,
    number,
    controlRequest,
    type Session,
  } from './state.svelte';
  import {
    preferences,
    detailLevel,
    setDetailLevel,
    cycleDetailLevels,
  } from './preferences.svelte';
  import Graph from './Graph.svelte';
  import SessionActions from './SessionActions.svelte';
  import StatusLine from './StatusLine.svelte';
  import GoalStatus from './GoalStatus.svelte';
  import SessionCopy from './SessionCopy.svelte';
  import SessionCommands from './SessionCommands.svelte';
  let copyControl: SessionCopy | undefined = $state();
  import FileDiff from './FileDiff.svelte';
  import MCPApproval from './MCPApproval.svelte';
  let { params = {} }: { params?: { id?: string } } = $props();
  let sessions = $derived(live.data?.sessions || []);
  let profiles = $derived(live.data?.control ? live.data.profiles || [] : []);
  let triggers = $derived(
    (live.data?.triggers || []).filter((t) =>
      sessions.some((s) => s.id === t.session),
    ),
  );
  function openTrigger(event: MouseEvent, id: string) {
    if (params.id && params.id !== id && nativeProtected) {
      event.preventDefault();
      return;
    }
    select(id);
  }
  function sessionStatus(session: Session) {
    return ['TURN COMPLETE', 'IDLE', 'ACTIVE'].includes(session.status) &&
      triggers.some((t) => t.session === session.id)
      ? 'TRIGGER SET'
      : session.status;
  }
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
  function isFailure(session: Session) {
    // Saved error text can outlive current attention (for example, after quit).
    // Use the shared backend classification, not historical context or labels.
    return session.status !== 'STALE' && session.failed === true;
  }
  // Parent rows already include linked-agent usage. Sum each listed row once;
  // do not add agent counts or activity samples to its observed token counter.
  let totals = $derived([
    ['OBSERVED TOKENS', number(sessions.reduce((sum, s) => sum + s.tokens, 0))],
    ['LISTED SESSIONS', number(sessions.length)],
    ...[
      ['WORKING', 'WORKING'],
      ['AWAITING APPROVAL', 'APPROVAL NEEDED'],
      ['AWAITING INPUT', 'INPUT NEEDED'],
      ['ERRORS', 'ERRORS'],
      ['CHECK · INFERRED', 'CHECK SESSION'],
    ].map(([label, status]) => [
      label,
      stale
        ? '—'
        : number(
            sessions.filter((s) =>
              status === 'ERRORS' ? isFailure(s) : s.status === status,
            ).length,
          ),
    ]),
  ]);
  let attention = $derived(
    sessions
      .filter(
        (s) =>
          ['INPUT NEEDED', 'APPROVAL NEEDED'].includes(s.status) ||
          isFailure(s),
      )
      .sort((a, b) => attentionRank(a) - attentionRank(b)),
  );
  function attentionRank(s: Session) {
    return s.status === 'APPROVAL NEEDED'
      ? 0
      : s.status === 'INPUT NEEDED'
        ? 1
        : 2;
  }
  let completed = $derived(
    sessions.filter(
      (s) =>
        s.status === 'TURN COMPLETE' &&
        (!s.readStateKnown || s.unread) &&
        !triggers.some((t) => t.session === s.id) &&
        !profiles.some((p) => p.session === s.id && p.pending),
    ),
  );

  let readNotice = $state('');
  const reading = new Set<string>();
  let readDwellSequence = 0;
  let readDwell = $state<{
    sequence: number;
    id: string;
    turn: string;
    revision: string;
    timer: ReturnType<typeof setTimeout>;
  }>();
  function readableResult(row: Session) {
    return (
      live.data?.control &&
      !stale &&
      row.unread &&
      row.readRevision &&
      row.replyTurn &&
      !row.streaming &&
      row.contextKind === 'LAST REPLY' &&
      !!row.text &&
      !profileFocused &&
      !profiles.some((p) => p.session === row.id && p.pending)
    );
  }
  function resultVisible(row: Session, replyOnly = false) {
    const scrollport = document.getElementById('content');
    const clip = scrollport?.getBoundingClientRect();
    const left = clip ? clip.left + scrollport!.clientLeft : 0;
    const top = clip ? clip.top + scrollport!.clientTop : 0;
    const right = left + (scrollport?.clientWidth || 0);
    const bottom = top + (scrollport?.clientHeight || 0);
    return Array.from(
      document.querySelectorAll<HTMLElement>('[data-result-session]'),
    ).some((node) => {
      const result = replyOnly ? node.querySelector('[data-reply-text]') : node;
      if (!result) return false;
      const rect = result.getBoundingClientRect();
      return (
        scrollport?.contains(node) &&
        node.dataset.resultSession === row.id &&
        node.dataset.resultTurn === row.replyTurn &&
        !node.closest('[inert], [hidden]') &&
        document.visibilityState === 'visible' &&
        rect.left < Math.min(window.innerWidth, right) &&
        rect.right > Math.max(0, left) &&
        rect.top < Math.min(window.innerHeight, bottom) &&
        rect.bottom > Math.max(0, top) &&
        rect.width > 0 &&
        rect.height > 0
      );
    });
  }
  function cancelReadDwell() {
    if (readDwell) clearTimeout(readDwell.timer);
    readDwell = undefined;
  }
  function wideResult(row: Session) {
    return (
      !params.id &&
      selectedID === row.id &&
      detailLevel(row.id) === 2 &&
      readableResult(row)
    );
  }
  function validateReadDwell() {
    if (!readDwell) return;
    const row = sessions.find((row) => row.id === readDwell!.id);
    if (
      !row ||
      !wideResult(row) ||
      row.replyTurn !== readDwell.turn ||
      row.readRevision !== readDwell.revision ||
      !resultVisible(row, true)
    )
      cancelReadDwell();
  }
  function startReadDwell(row: Session) {
    validateReadDwell();
    if (!wideResult(row) || !resultVisible(row, true) || readDwell) return;
    const id = row.id,
      turn = row.replyTurn!,
      revision = row.readRevision!;
    const sequence = ++readDwellSequence;
    readDwell = {
      sequence,
      id,
      turn,
      revision,
      timer: setTimeout(() => {
        validateReadDwell();
        if (
          !readDwell ||
          readDwell.sequence !== sequence ||
          readDwell.id !== id ||
          readDwell.turn !== turn ||
          readDwell.revision !== revision
        )
          return;
        const current = sessions.find((row) => row.id === id);
        cancelReadDwell();
        if (current) void reviewResult(current);
      }, 3000),
    };
  }
  // State changes only cancel; result arrival and restored layouts never start a timer.
  $effect(() => {
    validateReadDwell();
  });
  onMount(() => {
    document.addEventListener('scroll', validateReadDwell, true);
    document.addEventListener('visibilitychange', validateReadDwell);
    window.addEventListener('resize', validateReadDwell);
    return () => {
      cancelReadDwell();
      document.removeEventListener('scroll', validateReadDwell, true);
      document.removeEventListener('visibilitychange', validateReadDwell);
      window.removeEventListener('resize', validateReadDwell);
    };
  });
  async function reviewResult(row: Session) {
    if (!readableResult(row)) return;
    const id = row.id,
      revision = row.readRevision,
      turn = row.replyTurn;
    const key = JSON.stringify([id, revision, turn]);
    if (reading.has(key)) return;
    reading.add(key);
    try {
      await tick();
      const current = sessions.find((row) => row.id === id);
      if (
        !current ||
        !readableResult(current) ||
        !resultVisible(current) ||
        current.readRevision !== revision ||
        current.replyTurn !== turn
      )
        return;
      await controlRequest('read', { session: id, read: { turn, revision } });
      readNotice = '';
    } catch {
      readNotice =
        'Result changed or could not be marked read. Open it again to retry.';
    } finally {
      reading.delete(key);
    }
  }
  function reviewDisplayedResult(row: Session) {
    if (params.id === row.id) void reviewResult(row);
    else startReadDwell(row);
  }
  function openResult(id: string) {
    cancelReadDwell();
    select(id, false);
    if (params.id === id) {
      const row = sessions.find((row) => row.id === id);
      if (row) void reviewResult(row);
    }
  }
  // An arriving result in an already-open page needs a fresh review gesture.
  // Restoring a URL is not a review gesture, including when data arrives late.
  let resultRoute = $state(untrack(() => params.id || ''));
  let pendingResultRoute = $state('');
  $effect(() => {
    const id = params.id || '';
    if (id !== resultRoute) {
      resultRoute = id;
      pendingResultRoute = id;
    }
  });
  $effect(() => {
    if (pendingResultRoute && selected?.id === pendingResultRoute) {
      const row = selected;
      pendingResultRoute = '';
      untrack(() => {
        void reviewResult(row);
      });
    }
  });
  function select(id: string, review = true) {
    if (id !== selectedID) cancelReadDwell();
    preferences.selected = id;
    void tick().then(() => {
      document
        .querySelector('.session-row.selected')
        ?.scrollIntoView({ block: 'nearest' });
      const row = sessions.find((row) => row.id === id);
      if (review && row) startReadDwell(row);
    });
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
    else {
      setDetailLevel(id, level);
    }
  }
  function cycle() {
    cancelReadDwell();
    if (!params.id) cycleDetailLevels(sessions.map((session) => session.id));
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
    if (event.key.toLowerCase() === 'y' && !params.id) {
      event.preventDefault();
      if (!event.repeat) cycle();
    } else if (event.key === 'Escape' && params.id) {
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
    if (isFailure(session))
      return session.status === 'MODEL BUSY'
        ? 'CONFIRMED TURN FAILURE — the selected model was at capacity. Choose another model with /model or retry when ready. This is not a quota-exhaustion signal.'
        : session.status === 'MODEL UNAVAILABLE'
          ? 'CONFIRMED TURN FAILURE — the provider could not find the selected model. Choose an available model with /model. No automatic retry or model change.'
          : session.status === 'CAPACITY RESTRICTED'
            ? 'CONFIRMED TURN FAILURE — capacity is unavailable on a policy-restricted route. Inspect the error before changing settings or retrying. This is not a quota-exhaustion signal; no automatic retry or policy change.'
            : 'CONFIRMED TURN FAILURE — inspect the error below before sending another prompt. No automatic retry or model change.';
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
  function contextTitle(session: Session) {
    return session.streaming && session.contextKind === 'LAST REPLY'
      ? 'REPLY // STREAMING'
      : session.contextKind || 'LAST ACTIVITY';
  }
</script>

<svelte:window onkeydown={keydown} />

{#snippet context(session: Session, heading = true, full = false)}
  {#if session.currentTask}
    <h3>
      {session.contextKind === 'LAST REPLY' && !session.streaming
        ? 'TASK'
        : 'CURRENT TASK'}
    </h3>
    <pre>{session.currentTask}</pre>
  {/if}
  {#if session.latestGuidance}
    <h3>LATEST GUIDANCE</h3>
    <pre>{session.latestGuidance}</pre>
  {/if}
  {#if full && session.contextKind === 'APPROVAL REQUEST' && session.approvalContext}
    <h3>CONTEXT</h3>
    <pre>{session.approvalContext}</pre>
    <hr />
  {/if}
  {#if explanation(session) && (!full || stale || session.status === 'CHECK SESSION' || isFailure(session))}<p
      class="attention-note"
      class:inferred={session.status === 'CHECK SESSION'}
    >
      {explanation(session)}
    </p>{/if}
  {#if heading}<h3>
      {contextTitle(session)}
    </h3>{/if}
  {#if session.mcpTool && (!full || !live.data?.control)}
    <MCPApproval details={session.mcpTool} />
  {:else if session.text || !session.workingCommand}
    <pre data-reply-text>{session.text || 'No session context available.'}</pre>
  {/if}
  {#if session.workingCommand}
    <hr />
    <h3>
      COMMAND // {stale
        ? 'UNKNOWN'
        : (session.commandStatus || 'unknown').toUpperCase()}
      {#if session.commandTiming}
        // {session.commandTiming}{/if}
      {#if !stale && (session.runningCommands || 0) > (session.commandStatus === 'running' ? 1 : 0)}
        // +{(session.runningCommands || 0) -
          (session.commandStatus === 'running' ? 1 : 0)} RUNNING
      {/if}
      {#if session.runningLimited}+{/if}
    </h3>
    <pre class="command">{session.workingCommand}</pre>
  {/if}
  {#if (!full || !live.data?.control) && session.fileChanges?.length}
    <FileDiff lines={session.fileChanges} />
  {:else if (!full || !live.data?.control) && !session.mcpTool && (session.command || session.status === 'APPROVAL NEEDED')}
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

{#if readNotice}<p class="notice" role="status">{readNotice}</p>{/if}
<div class="spread">
  <h1>SESSION TOTALS</h1>
  <span class="eyebrow">OBSERVED {date(live.data?.sessionsAt)}</span>
</div>
<dl class="session-totals" aria-label="Session totals" class:stale>
  {#each totals as [label, value]}<div
      class:failed={label === 'ERRORS' && value !== '0' && value !== '—'}
    >
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
{#if (attention.length || completed.length || profiles.some((p) => p.pending) || triggers.length) && !stale}<nav
    class="attention-summary"
    aria-label="Sessions needing attention"
  >
    {#each attention as session}<a
        class="button"
        class:approval={session.status === 'APPROVAL NEEDED' ||
          session.status === 'INPUT NEEDED' ||
          isFailure(session)}
        href={'#/sessions/' + encodeURIComponent(session.id)}
        onclick={() => select(session.id)}
        >{session.status}
        {Array.from(session.id).slice(-5).join('').toUpperCase()} // {session.name ||
          session.directory ||
          session.id}</a
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
        >QUOTA THRESHOLD {Array.from(profile.session)
          .slice(-5)
          .join('')
          .toUpperCase()} // {sessions.find((s) => s.id === profile.session)
          ?.name ||
          sessions.find((s) => s.id === profile.session)?.directory ||
          profile.session}</a
      >
    {/each}
    {#each completed as session}<a
        class="button"
        href={'#/sessions/' + encodeURIComponent(session.id)}
        onclick={() => select(session.id)}
        >TURN COMPLETE {Array.from(session.id).slice(-5).join('').toUpperCase()} //
        {session.name || session.directory || session.id}</a
      >{/each}
    {#each triggers as trigger}<a
        class="button"
        href={'#/sessions/' + encodeURIComponent(trigger.session)}
        onclick={(event) => openTrigger(event, trigger.session)}
        >TRIGGER SET {Array.from(trigger.session)
          .slice(-5)
          .join('')
          .toUpperCase()} // {sessions.find((s) => s.id === trigger.session)
          ?.name ||
          sessions.find((s) => s.id === trigger.session)?.directory}</a
      >{/each}
  </nav>{/if}
{#if params.id}
  {#if selected}<section class="panel full-detail">
      <div class="detail-heading">
        <h2
          class:failed={isFailure(selected) && !stale && !profileFocused}
          class:reviewed={selected.status === 'TURN COMPLETE' &&
            selected.readStateKnown &&
            !selected.unread}
        >
          <span
            class="lamp lit"
            class:failed={isFailure(selected) && !stale}
            class:working={selected.status === 'WORKING' && !stale}
          ></span>{stale
            ? 'STALE'
            : profileFocused
              ? 'QUOTA THRESHOLD'
              : sessionStatus(selected)} // {selected.name ||
            selected.directory}
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
        <div
          class="detail-context"
          data-result-session={selected.id}
          data-result-turn={selected.replyTurn}
          onwheel={() => {
            reviewDisplayedResult(selected);
          }}
        >
          {@render context(selected, true, true)}
        </div>
        {#if live.data?.control}
          {#key selected.id}<SessionActions
              session={selected.id}
              oncopy={() => copyControl?.copy()}
              openScheduler={new URLSearchParams(router.querystring).get(
                'schedule',
              ) === 'edit'}
              observedCommand={selected.command}
              observedFileChanges={selected.fileChanges || []}
              suspended={profileFocused}
              onProtectedChange={(value) => {
                nativeProtected = value;
              }}
            />{/key}
        {:else}<div>
            <p class="notice">Read only — reply or approve in Codex.</p>
            {#key selected.id}<SessionCommands
                session={selected.id}
                localOnly
                oncopy={() => copyControl?.copy()}
              />{/key}
          </div>{/if}
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
      {#if selected.goal}
        {#key selected.id}<StatusLine session={selected} {stale}>
            {#snippet children()}
              {#if !profileFocused}<SessionCopy
                  bind:this={copyControl}
                  session={selected}
                  active
                  compact
                />{/if}
            {/snippet}
          </StatusLine>{/key}
      {:else}
        {#if !profileFocused}{#key selected.id}<SessionCopy
              bind:this={copyControl}
              session={selected}
              active
            />{/key}{/if}
        {#key selected.id}<StatusLine session={selected} {stale} />{/key}
      {/if}
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
      <button
        onclick={cycle}
        disabled={!sessions.length}
        title="Match the most detailed row, then cycle graph, split and wide views"
        >C(Y)CLE</button
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
              >{Array.from(session.id).slice(-5).join('').toUpperCase()} // {session.name}</button
            >
          </h2>
        {/if}
        <h2>
          <a
            class="status-detail"
            class:reviewed={session.status === 'TURN COMPLETE' &&
              session.readStateKnown &&
              !session.unread}
            class:failed={isFailure(session) && !stale}
            href={'#/sessions/' + encodeURIComponent(session.id)}
            onclick={() => openResult(session.id)}
          >
            <span
              class="lamp lit"
              class:failed={isFailure(session) && !stale}
              class:working={session.status === 'WORKING' && !stale}
            ></span>{stale ? 'STALE' : sessionStatus(session)}
          </a>
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
        <GoalStatus goal={session.goal} {stale} />
        <div class="detail-controls">
          <button
            aria-label="Less detail"
            disabled={level === 0}
            onclick={() => change(session.id, -1)}>←</button
          >
          <button
            onclick={() => {
              const opening = level === 0;
              select(session.id);
              setDetailLevel(session.id, opening ? 1 : 0);
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
          onclick={() => openResult(session.id)}>FULL DETAIL →</a
        >
        {#if !stale && (['APPROVAL NEEDED', 'INPUT NEEDED', 'CHECK SESSION'].includes(session.status) || isFailure(session))}<p
            class="attention-note"
          >
            {session.status === 'CHECK SESSION'
              ? 'INFERRED INACTIVITY'
              : isFailure(session)
                ? 'OPEN DETAIL FOR THE TURN ERROR'
                : live.data?.control
                  ? 'OPEN FULL DETAIL OR REPLY IN CODEX'
                  : session.status === 'APPROVAL NEEDED'
                    ? 'APPROVE OR DECLINE IN CODEX'
                    : 'REPLY IN CODEX'}
          </p>{/if}
      </div>
      {#if level > 0}<div class="panel context">
          <div class="spread">
            <h2>{contextTitle(session)}</h2>
            {#if !stale && session.status === 'APPROVAL NEEDED'}<a
                class="attention-badge"
                href={'#/sessions/' + encodeURIComponent(session.id)}
                >{live.data?.control
                  ? 'REVIEW IN FULL DETAIL ↗'
                  : 'APPROVAL IN CODEX ↗'}</a
              >{/if}
          </div>
          <div
            data-result-session={session.id}
            data-result-turn={session.replyTurn}
            onwheel={() => {
              reviewDisplayedResult(session);
            }}
          >
            {@render context(session, false)}
          </div>
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

<style>
  .reviewed {
    color: var(--muted);
    font-weight: normal;
  }
  .reviewed .lamp {
    opacity: 0.35;
  }
  .failed {
    color: var(--approval);
  }
  .lamp.failed {
    background: var(--approval);
    box-shadow: 0 0 8px color-mix(in srgb, var(--approval) 40%, transparent);
  }
</style>
