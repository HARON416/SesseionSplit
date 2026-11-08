import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
export default defineConfig({
  server: { host: '127.0.0.1', port: Number(process.env.WAILS_VITE_PORT) || 9246, strictPort: true },
  plugins: [svelte()],
});
