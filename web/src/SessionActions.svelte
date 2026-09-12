<script lang="ts">
  import { onMount } from 'svelte';
  import { live, controlRequest } from './state.svelte';
  let {
    session,
    observedCommand = '',
  }: { session: string; observedCommand?: string } = $props();
  interface Offer {
    id: string;
    session: string;
    thread: string;
    directory: string;
    kind: string;
    command: string;
    choices: { label: string; detail: string; persistent: boolean }[] | null;
    questions:
      | {
          text: string;
          secret: boolean;
          freeText: boolean;
          options: string[] | null;
        }[]
      | null;
  }
  let offer = $state<Offer | null>(null);
  let answers = $state<string[]>([]);
  let choice = $state<number | null>(null);
  let confirmation = $state('');
  let expires = $state(0);
  let now = $state(Date.now());
  let busy = $state(false);
  let notice = $state('');
  let success = $state(false);
  let offerError = $state(false);
  let sent = $state('');
  let status = $derived(
    live.data?.sessions.find((row) => row.id === session)?.status,
  );
  let stale = $derived(!live.connected || !!live.data?.sessionsError);
  let actionableCommand = $derived(
    !stale &&
      !offerError &&
      !!offer?.id &&
      offer.kind === 'approval' &&
      sent !== offer.id,
  );
  let command = $derived(actionableCommand ? offer!.command : observedCommand);
  let confirming = $derived(!!confirmation && now < expires && !stale);
  let questions = $derived(
    offer?.questions?.length
      ? offer.questions
      : [
          {
            text: 'Follow-up message',
            secret: false,
            freeText: true,
            options: [],
          },
        ],
  );
  let valid = $derived(
    offer?.kind === 'approval'
      ? choice !== null
      : answers.length === questions.length &&
          answers.every(
            (a, index) =>
              a.trim().length > 0 &&
              (questions[index].freeText ||
                questions[index].options?.includes(a)),
          ),
  );
  $effect(() => {
    if (stale || now >= expires) confirmation = '';
  });
  const controller = new AbortController();
  onMount(() => {
    let timer: ReturnType<typeof setTimeout>;
    const clock = setInterval(() => {
      now = Date.now();
    }, 1000);
    async function poll() {
      try {
        const next = await controlRequest<Offer>(
          'offer',
          { session },
          controller.signal,
        );
        if (controller.signal.aborted) return;
        offerError = false;
        if (offer?.id !== next.id) {
          confirmation = '';
          choice = null;
          answers = Array(Math.max(1, next.questions?.length || 0)).fill('');
        }
        offer = next;
        if (success && next.id && next.id !== sent) {
          notice = '';
          success = false;
        }
      } catch {
        if (!controller.signal.aborted) {
          offer = null;
          offerError = true;
          confirmation = '';
        }
      } finally {
        if (!controller.signal.aborted) timer = setTimeout(poll, 2000);
      }
    }
    void poll();
    return () => {
      controller.abort();
      clearTimeout(timer);
      clearInterval(clock);
    };
  });
  async function prepare() {
    if (!offer?.id || busy || stale || !valid) return;
    const id = offer.id;
    busy = true;
    notice = '';
    success = false;
    confirmation = '';
    try {
      const result = await controlRequest<{
        confirmation: string;
        expires: string;
      }>(
        'prepare',
        {
          session,
          offer: id,
          ...(offer.kind === 'approval'
            ? { choice }
            : { answers: [...answers] }),
        },
        controller.signal,
      );
      if (offer?.id === id && !controller.signal.aborted && !stale) {
        confirmation = result.confirmation;
        expires = Date.parse(result.expires);
      }
    } catch (error) {
      if (!controller.signal.aborted)
        notice =
          error instanceof Error ? error.message : 'Unable to prepare action.';
    } finally {
      busy = false;
    }
  }
  async function commit() {
    if (!offer?.id || busy || !confirming) return;
    const id = offer.id;
    const kind = offer.kind;
    const ticket = confirmation;
    confirmation = '';
    busy = true;
    sent = id;
    try {
      await controlRequest(
        'commit',
        { session, offer: id, confirmation: ticket },
        controller.signal,
      );
      success = true;
      notice = kind === 'approval' ? 'Decision sent.' : 'Text sent.';
    } catch {
      notice =
        'Outcome uncertain. Check Codex before taking another action; nothing was retried.';
    } finally {
      busy = false;
      answers = [];
      choice = null;
    }
  }
</script>

<section class="session-actions" aria-label="Session controls">
  <h3>SESSION CONTROL // EXPERIMENTAL</h3>
  {#if notice}<p class:notice={!success} class:sent={success} role="status">
      {notice}
    </p>{/if}
  {#if command}
    <h3>{actionableCommand ? 'EXACT COMMAND' : 'LAST OBSERVED COMMAND'}</h3>
    <pre class="command">{command}</pre>
  {:else if status === 'APPROVAL NEEDED' && !success}
    <p class="muted">
      Command unavailable from this observation. Open Codex to inspect the
      request.
    </p>
  {/if}
  {#if stale || offerError}
    <p class="muted">
      Session controls temporarily unavailable. Check Codex for current state.
    </p>
  {:else if !offer}
    <p class="muted">Checking session controls…</p>
  {:else if !offer.id}
    {#if status === 'WORKING' || (!success && !busy)}
      <p class="muted">
        {status === 'WORKING'
          ? 'Codex is working — nothing to respond to.'
          : ['APPROVAL NEEDED', 'INPUT NEEDED'].includes(status || '')
            ? 'Respond in Codex for this request.'
            : 'Nothing needs a response right now.'}
      </p>
    {/if}
    {#if ['APPROVAL NEEDED', 'INPUT NEEDED'].includes(status || '') && !success}
      <details>
        <summary>About browser controls</summary>
        <p class="muted">
          Controls require a supported live request from a connected shared
          app-server session. Local observations alone cannot provide them.
        </p>
      </details>
    {/if}
  {:else if sent !== offer.id}
    <p class="muted">
      TARGET // {offer.thread} // {offer.directory || 'Directory unavailable'}
    </p>
    <fieldset disabled={busy || confirming || stale}>
      <legend
        >{offer.kind === 'approval'
          ? 'Choose a decision'
          : 'Reply to this session'}</legend
      >
      {#if offer.kind === 'approval'}
        {#each offer.choices || [] as option, index}
          <label class="decision"
            ><input
              type="radio"
              name="decision"
              value={index}
              bind:group={choice}
            />
            {option.label}
            {#if option.detail}<code>{option.detail}</code>{/if}
            {#if option.persistent}<span class="notice"
                >Grants permission beyond this one command. Check the scope
                carefully.</span
              >{/if}
          </label>
        {/each}
      {:else}
        {#each questions as question, index}
          <label class="answer"
            >{question.text}
            {#if question.secret}
              <input
                type="password"
                autocomplete="off"
                maxlength="4096"
                bind:value={answers[index]}
              />
            {:else if !question.freeText}
              <select bind:value={answers[index]}
                ><option value="">Choose an answer…</option
                >{#each question.options || [] as option}<option value={option}
                    >{option}</option
                  >{/each}</select
              >
            {:else}
              <textarea
                rows="3"
                maxlength="4096"
                autocomplete="off"
                bind:value={answers[index]}></textarea>
              {#if question.options?.length}<p class="muted">
                  Suggested answers: {question.options.join(' · ')}
                </p>{/if}
            {/if}
          </label>
          {#if question.secret && !question.freeText}
            <details>
              <summary>View fixed choices</summary>
              <p class="muted">
                Type one of these choices exactly. Your answer stays masked.
              </p>
              <ul>
                {#each question.options || [] as option}<li>{option}</li>{/each}
              </ul>
            </details>
          {/if}
        {/each}
      {/if}
    </fieldset>
    {#if confirming}
      <p class="notice">
        Check the target and {offer.kind === 'approval'
          ? 'exact command and permission scope'
          : 'message above'}. This will send to Codex; it may start work using
        your quota. Confirmation expires in {Math.max(
          0,
          Math.ceil((expires - now) / 1000),
        )}s.
      </p>
      <button onclick={commit} disabled={busy || stale}
        >CONFIRM {offer.kind === 'approval'
          ? offer.choices?.[choice!]?.label
          : 'SEND'}</button
      >
      <button
        onclick={() => {
          confirmation = '';
        }}>CANCEL</button
      >
    {:else}
      <button onclick={prepare} disabled={busy || stale || !valid}
        >{busy ? 'SENDING…' : 'REVIEW BEFORE SENDING'}</button
      >
    {/if}
  {/if}
</section>

<style>
  fieldset {
    border: 1px solid currentColor;
    margin: 0.5rem 0;
    padding: 0.35rem 0.6rem;
  }
  .decision,
  .answer {
    display: block;
    margin: 0.3rem 0;
  }
  .decision code,
  .decision span {
    display: block;
    margin: 0.3rem 0 0.3rem 1.5rem;
    overflow-wrap: anywhere;
  }
  textarea,
  .answer input,
  .answer select {
    display: block;
    box-sizing: border-box;
    width: 100%;
    margin-top: 0.4rem;
    background: var(--bg);
    color: inherit;
    border: 1px solid currentColor;
    font: inherit;
    padding: 0.5rem;
  }
  textarea {
    resize: vertical;
  }
  button {
    margin: 0.3rem 0.5rem 0.3rem 0;
  }
  h3,
  p {
    margin-block: 0.4rem;
  }
  .sent {
    color: var(--accent);
  }
</style>
