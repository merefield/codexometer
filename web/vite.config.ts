import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';

export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/web/dist',
    emptyOutDir: true,
    license: { fileName: 'assets/THIRD-PARTY-LICENSES.md' },
  },
});
