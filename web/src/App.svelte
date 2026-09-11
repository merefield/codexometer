<script lang="ts">
  import { onMount } from 'svelte';
  import Router, { router } from 'svelte-spa-router';
  import { connect, live } from './state.svelte';
  import Quota from './Quota.svelte';
  import Sessions from './Sessions.svelte';
  import Usage from './Usage.svelte';
  import Missing from './Missing.svelte';

  const routes = {
    '/': Quota,
    '/quota/:view?': Quota,
    '/sessions/:id?': Sessions,
    '/usage': Usage,
    '*': Missing,
  };
  const tabPaths = {
    quota: /^(?:\/|\/quota(?:\/[^/]+)?\/?)$/,
    sessions: /^\/sessions(?:\/[^/]+)?\/?$/,
    usage: /^\/usage\/?$/,
  };
  let theme = $state('hacker');
  const themes = ['hacker', 'rust', 'blue-steel', 'ultraviolet', 'nightshade'];
  onMount(() => {
    try {
      const saved = localStorage.getItem('codexometer.web.theme');
      if (saved && themes.includes(saved)) theme = saved;
    } catch {
      /* Optional preference. */
    }
    return connect();
  });
  function changeTheme() {
    try {
      localStorage.setItem('codexometer.web.theme', theme);
    } catch {
      /* Optional preference. */
    }
  }
</script>

<div class="shell" data-theme={theme}>
  <header>
    <div>
      <a class="brand" href="#/quota/bars">CODEXOMETER</a>
      <p>Your quota. Your sessions. Your command centre.</p>
    </div>
    <div class="connection">
      <span class:lit={live.connected} class="lamp"></span>{live.connected
        ? 'CONNECTED'
        : 'OFFLINE'}<small>EXPERIMENTAL // READ ONLY</small>
    </div>
  </header>
  <nav aria-label="Main navigation">
    {#each Object.entries(tabPaths) as [tab, pattern]}
      {@const current = pattern.test(router.location)}
      <a
        class:active={current}
        href={'#/' + tab}
        aria-current={current ? 'page' : undefined}>{tab.toUpperCase()}</a
      >
    {/each}
  </nav>
  <main id="content">
    {#if live.error}<p class="notice" role="status">{live.error}</p>{/if}
    {#if live.data}<Router {routes} />{:else if !live.error}<p class="empty">
        Connecting to your local Codexometer…
      </p>{/if}
  </main>
  <footer>
    <span>v{live.data?.version || '…'} // LOCAL ONLY</span>
    <span>No actions can be sent from this preview.</span>
    <label
      >THEME // <select
        aria-label="Theme"
        bind:value={theme}
        onchange={changeTheme}
        >{#each themes as name}<option value={name}
            >{name.replace('-', ' ').toUpperCase()}</option
          >{/each}</select
      ></label
    >
  </footer>
</div>
