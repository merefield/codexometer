<script lang="ts">
  import { onDestroy, tick } from 'svelte';
  import { controlRequest, live } from './state.svelte';
  import { statusLineUI } from './preferences.svelte';
  let {
    session,
    query = '',
    onchange = () => {},
    oncomplete = (_text: string) => {},
  }: {
    session: string;
    query?: string;
    onchange?: () => void;
    oncomplete?: (text: string) => void;
  } = $props();
  interface Choice {
    id: string;
    label: string;
    help: string;
    next?: string;
    action?: boolean;
  }
  interface Menu {
    title: string;
    help: string;
    path: string;
    revision: string;
    choices: Choice[];
  }
  let open = $state(false),
    busy = $state(false),
    notice = $state('');
  let menu = $state<Menu | null>(null),
    selected = $state<Choice | null>(null);
  let confirmation = $state(''),
    expires = $state(0),
    now = $state(Date.now());
  let request = 0;
  let selectedIndex = $state(0);
  let dismissedQuery = $state<string | null>(null);
  let optionList: HTMLDivElement | undefined = $state();
  const clock = setInterval(() => {
    now = Date.now();
  }, 1000);
  onDestroy(() => {
    clearInterval(clock);
    request++;
  });
  let slash = $derived(
    query.trim().startsWith('/') && !query.trim().startsWith('//'),
  );
  let filter = $derived(
    menu?.path === '' && slash ? query.trim().slice(1).toLowerCase() : '',
  );
  let choices = $derived(
    (menu?.choices || []).filter(
      (o) => !filter || o.label.toLowerCase().startsWith('/' + filter),
    ),
  );
  let unavailable = $derived(!live.connected || !!live.data?.sessionsError);
  let suggesting = $derived(
    open && slash && (!menu || menu.path === '') && !selected,
  );
  $effect(() => {
    query;
    selectedIndex = 0;
  });
  $effect(() => {
    const index = selectedIndex;
    choices;
    if (suggesting)
      void tick().then(() => {
        const row = optionList?.children[index] as HTMLElement | undefined;
        if (!row || !optionList) return;
        const listRect = optionList.getBoundingClientRect();
        const rowRect = row.getBoundingClientRect();
        if (rowRect.top < listRect.top)
          optionList.scrollTop += rowRect.top - listRect.top;
        else if (rowRect.bottom > listRect.bottom)
          optionList.scrollTop += rowRect.bottom - listRect.bottom;
      });
  });
  export function handleKey(event: KeyboardEvent) {
    if (
      !slash ||
      !open ||
      (menu && menu.path !== '') ||
      selected ||
      unavailable ||
      event.isComposing ||
      event.ctrlKey ||
      event.metaKey ||
      event.altKey
    )
      return;
    if (!['ArrowUp', 'ArrowDown', 'Tab', 'Enter', 'Escape'].includes(event.key))
      return;
    if (event.key === 'Tab' && event.shiftKey) return;
    event.preventDefault();
    if (event.key === 'Escape') {
      open = false;
      dismissedQuery = query;
      confirmation = '';
      return;
    }
    if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
      selectedIndex = Math.min(
        Math.max(selectedIndex + (event.key === 'ArrowUp' ? -1 : 1), 0),
        Math.max(choices.length - 1, 0),
      );
      return;
    }
    const option = choices[selectedIndex];
    if (!option || busy) return;
    if (event.key === 'Tab') oncomplete(option.label);
    else void choose(option);
  }
  $effect(() => {
    if (unavailable) {
      request++;
      busy = false;
      confirmation = '';
    }
  });
  $effect(() => {
    if (slash && !open && query !== dismissedQuery && !unavailable) {
      open = true;
      void load('');
    }
  });
  async function load(path: string) {
    const seq = ++request;
    busy = true;
    selected = null;
    confirmation = '';
    notice = '';
    try {
      const result = await controlRequest<Menu>('commands', {
        session,
        command: { mode: 'list', path },
      });
      if (seq === request) {
        if (path === '' || path === 'help')
          result.choices.push({
            id: 'local-statusline',
            label: '/statusline',
            help: 'Choose multiple fields for Codexometer’s detail footer.',
            next: 'statusline',
          });
        menu = result;
      }
    } catch {
      if (seq === request) {
        notice = 'Command catalogue unavailable. Use Codex.';
        if (path === '' || path === 'help')
          menu = {
            title: '/ COMMANDS',
            help: 'Local display settings remain available.',
            path: '',
            revision: '',
            choices: [
              {
                id: 'local-statusline',
                label: '/statusline',
                help: 'Choose multiple fields for Codexometer’s detail footer.',
                next: 'statusline',
              },
            ],
          };
      }
    } finally {
      if (seq === request) busy = false;
    }
  }
  async function choose(o: Choice) {
    if (o.next === 'statusline') {
      statusLineUI.open = true;
      open = false;
      dismissedQuery = query;
      return;
    }
    if (o.next) {
      await load(o.next);
      return;
    }
    selected = o;
    confirmation = '';
    notice = '';
    if (!o.action || !menu) return;
    const seq = ++request;
    busy = true;
    try {
      const result = await controlRequest<{
        confirmation: string;
        expires: string;
      }>('commands', {
        session,
        command: {
          mode: 'prepare',
          path: menu.path,
          revision: menu.revision,
          choice: o.id,
        },
      });
      if (seq === request) {
        confirmation = result.confirmation;
        expires = Date.parse(result.expires);
      }
    } catch {
      if (seq === request)
        notice = 'Option changed or unavailable. Refresh before retrying.';
    } finally {
      if (seq === request) busy = false;
    }
  }
  async function commit() {
    if (!confirmation || unavailable || busy || now >= expires || !menu) return;
    const token = confirmation;
    confirmation = '';
    busy = true;
    const seq = ++request;
    try {
      const result = await controlRequest<{ message: string }>('commands', {
        session,
        confirmation: token,
        command: { mode: 'commit', path: menu.path },
      });
      if (seq === request) {
        notice = result.message;
        selected = null;
        onchange();
      }
    } catch {
      if (seq === request)
        notice = 'Change unconfirmed. Check Codex before retrying.';
    } finally {
      if (seq === request) busy = false;
    }
  }
</script>

<section aria-label="Session slash commands" class="slash-commands">
  <button
    disabled={unavailable || busy}
    onclick={() => {
      open = !open;
      dismissedQuery = open ? null : query;
      confirmation = '';
      if (open) void load('');
    }}
  >
    / COMMANDS
  </button>
  {#if open}
    <div
      class:command-popup={suggesting}
      aria-label={suggesting ? 'Slash command suggestions' : undefined}
    >
      {#if !suggesting}
        <h3>{menu?.title || '/ COMMANDS'}</h3>
        <p class="muted">{menu?.help || 'Loading live options…'}</p>
      {/if}
      {#if notice}<p role="status">{notice}</p>{/if}
      {#if busy}<p role="status">Loading / sending…</p>{/if}
      {#if selected}
        <h4>{selected.label}</h4>
        <pre>{selected.help || 'No additional help supplied by Codex.'}</pre>
        {#if selected.action}<p class="notice">
            TARGET // {session}. Changes affect subsequent turns of this
            session, not global defaults. Automatic quota thresholds may later
            supersede model settings.
          </p>
          <button
            disabled={busy || unavailable || !confirmation || now >= expires}
            onclick={commit}>CONFIRM CHANGE</button
          >
          {#if confirmation && now >= expires}<p>
              Confirmation expired. Reopen the option.
            </p>{/if}
        {/if}
        <button
          disabled={busy}
          onclick={() => {
            selected = null;
            confirmation = '';
          }}>BACK</button
        >
      {:else}
        <div
          class="command-options"
          class:vertical={suggesting}
          bind:this={optionList}
        >
          {#each choices as o, index (o.id)}<button
              disabled={busy || unavailable}
              title={o.help}
              aria-label={o.label +
                (o.next ? ' →' : !o.action ? ' // HELP' : '')}
              class:suggested={slash &&
                menu?.path === '' &&
                selectedIndex === index}
              onclick={() => choose(o)}
              >{o.label}{o.next
                ? ' →'
                : !o.action
                  ? ' // HELP'
                  : ''}{#if slash && menu?.path === ''}<span
                  class="command-help">{o.help}</span
                >{/if}</button
            >{/each}
        </div>
        {#if !busy && !choices.length}<p>
            No available matching commands.
          </p>{/if}
        {#if slash && menu?.path === ''}<p class="muted">
            ↑/↓ select · Tab complete · Enter options · Escape dismiss
          </p>{/if}
      {/if}
      {#if !suggesting}
        <button disabled={busy} onclick={() => load('')}>ALL COMMANDS</button>
        <button disabled={busy} onclick={() => load(menu?.path || '')}
          >REFRESH OPTIONS</button
        >
      {/if}
    </div>
  {/if}
</section>

<style>
  .slash-commands {
    margin: 0.4rem 0;
    position: relative;
  }
  .command-popup {
    position: absolute;
    bottom: 100%;
    left: 0;
    z-index: 5;
    width: min(40rem, 100%);
    box-sizing: border-box;
    padding: 0.35rem;
    border: 1px solid var(--accent);
    background: var(--panel);
    box-shadow: 0 0.3rem 1rem #0006;
  }
  .command-popup p {
    margin: 0.3rem 0;
    font-size: 0.8em;
  }
  .command-options.vertical {
    display: flex;
    flex-direction: column;
    flex-wrap: nowrap;
    gap: 0.15rem;
  }
  .vertical button {
    flex: none;
    text-align: left;
    width: 100%;
  }
  .vertical .command-help {
    max-width: none;
  }
  .command-options {
    display: flex;
    flex-wrap: wrap;
    gap: 0.35rem;
    max-height: 35vh;
    overflow: auto;
  }
  .command-options .suggested {
    outline: 1px solid var(--accent);
  }
  .command-help {
    display: block;
    font-size: 0.8em;
    max-width: 32ch;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    opacity: 0.75;
  }
  pre {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    max-height: 35vh;
    overflow: auto;
  }
</style>
