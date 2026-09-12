<script lang="ts">
  import { onMount } from 'svelte';
  import { live, controlRequest } from './state.svelte';
  let { session }: { session: string } = $props();
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
  let sent = $state('');
  let stale = $derived(!live.connected || !!live.data?.sessionsError);
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
          answers.every((a) => a.trim().length > 0),
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
        if (offer?.id !== next.id) {
          confirmation = '';
          choice = null;
          answers = Array(Math.max(1, next.questions?.length || 0)).fill('');
        }
        offer = next;
      } catch {
        if (!controller.signal.aborted) {
          offer = null;
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
      notice = 'Sent. Waiting for Codex to update…';
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
  <hr />
  <h3>SESSION CONTROL // EXPERIMENTAL</h3>
  {#if notice}<p class="notice" role="status">{notice}</p>{/if}
  {#if stale || !offer?.id}
    <p class="muted">
      No supported live action available. Reply or approve in Codex. Browser
      controls require a connected shared app-server session, not just local
      observations.
    </p>
  {:else if sent !== offer.id}
    <p class="muted">
      TARGET // {offer.thread} // {offer.directory || 'Directory unavailable'}
    </p>
    {#if offer.command}<h3>EXACT COMMAND</h3>
      <pre class="command">{offer.command}</pre>{/if}
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
            {#if !question.freeText}
              <select bind:value={answers[index]}
                ><option value="">Choose an answer…</option
                >{#each question.options || [] as option}<option value={option}
                    >{option}</option
                  >{/each}</select
              >
            {:else if question.secret}
              <input
                type="password"
                autocomplete="off"
                maxlength="4096"
                bind:value={answers[index]}
              />
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
    padding: 0.75rem;
  }
  .decision,
  .answer {
    display: block;
    margin: 0.5rem 0;
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
</style>
