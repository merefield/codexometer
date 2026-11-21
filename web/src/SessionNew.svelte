<script lang="ts">
  import { onMount, tick } from 'svelte';
  import {
    ControlRejected,
    live,
    resumePicker,
    startSession,
  } from './state.svelte';
  import { requiresConfirmation } from './confirmation';

  let {
    scope,
    liveIDs,
    onclose,
  }: { scope: string; liveIDs: Set<string>; onclose: () => void } = $props();
  let directory = $state('');
  let prompt = $state('');
  let draft = $state<HTMLTextAreaElement>();
  let busy = $state(false);
  let uncertainty = $state(false);
  let notice = $state('');
  let confirmation = $state('');
  let pendingID = $state('');
  let firstMessageRejected = $state(false);
  let timeout: ReturnType<typeof setTimeout>;
  let available = $derived(
    live.connected &&
      live.data?.control &&
      !live.data.sessionsError &&
      live.data.savedScope === scope,
  );
  let locked = $derived(busy || !!pendingID || !!confirmation || uncertainty);
  let readyToOpen = $derived(
    !!pendingID && available && liveIDs.has(pendingID),
  );

  onMount(() => {
    draft?.focus();
    return () => clearTimeout(timeout);
  });
  $effect(() => {
    if (!resumePicker.open) void tick().then(() => draft?.focus());
  });
  $effect(() => {
    if (readyToOpen && !firstMessageRejected) openSession();
  });
  function openSession() {
    location.hash = '/sessions/' + encodeURIComponent(pendingID);
    onclose();
  }
  async function send() {
    if (!available || !startSession || busy || pendingID || uncertainty) return;
    const value = prompt.trim();
    if (value.toLowerCase() === '/resume') {
      resumePicker.open = true;
      return;
    }
    if (!value || (value.startsWith('/') && !value.startsWith('//'))) {
      notice = 'Type a first message, or use /resume to open a saved session.';
      return;
    }
    if (!directory) {
      notice = 'Enter an absolute working directory on the Codex machine.';
      return;
    }
    const preparing =
      requiresConfirmation(live.data?.confirmationMode, true) && !confirmation;
    const token = confirmation;
    busy = true;
    notice = '';
    try {
      const result = await startSession(
        scope,
        directory,
        prompt,
        preparing
          ? { mode: 'prepare' }
          : token
            ? { confirmation: token }
            : undefined,
      );
      if (preparing && result.confirmation) {
        confirmation = result.confirmation;
      } else if (result.id) {
        firstMessageRejected = result.firstMessageRejected === true;
        pendingID = result.id;
        if (firstMessageRejected) {
          notice =
            'Session created, but Codex rejected the first message. Copy your draft before opening the session to try again.';
        } else {
          notice = 'Opening the new session…';
          timeout = setTimeout(() => {
            notice =
              'First message sent; still waiting for the session to appear.';
          }, 15000);
        }
      } else {
        throw new Error('Could not verify the new session. Check Codex.');
      }
    } catch (error) {
      uncertainty = !preparing && !(error instanceof ControlRejected);
      notice = uncertainty
        ? 'Could not verify the new session. Check Codex before trying again.'
        : error instanceof Error
          ? error.message
          : 'Could not start the new session.';
    } finally {
      busy = false;
      if (!preparing) confirmation = '';
    }
  }
</script>

<section class="panel" aria-label="New session" hidden={resumePicker.open}>
  <div class="spread">
    <h2>NEW SESSION</h2>
    <button disabled={busy} onclick={onclose}>Close</button>
  </div>
  <label>
    Working directory
    <input
      bind:value={directory}
      readonly={locked}
      placeholder="Absolute path on the Codex machine"
    />
  </label>
  <label>
    First message
    <textarea
      class="composer-surface"
      bind:this={draft}
      bind:value={prompt}
      maxlength={4096}
      readonly={locked}
      onkeydown={(event) => {
        if (locked) return;
        if (event.key === 'Enter' && !event.shiftKey && !event.isComposing) {
          event.preventDefault();
          if (!event.repeat) void send();
        }
        if (event.key === 'Escape' && !locked) onclose();
      }}></textarea>
  </label>
  {#if confirmation}
    <p>Start a new Codex session and send this message?</p>
    <button disabled={busy || !available} onclick={() => void send()}
      >Confirm start</button
    >
    <button disabled={busy} onclick={() => (confirmation = '')}>Cancel</button>
  {:else}
    <button disabled={locked || !available} onclick={() => void send()}
      >Send first message</button
    >
    <button
      disabled={locked || !available}
      onclick={() => (resumePicker.open = true)}>Saved sessions</button
    >
  {/if}
  {#if firstMessageRejected && readyToOpen}
    <button onclick={openSession}>Open session</button>
  {/if}
  {#if notice}<p role="status">{notice}</p>{/if}
</section>
