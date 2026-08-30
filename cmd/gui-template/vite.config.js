import { fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'
export default defineConfig({ root: fileURLToPath(new URL('.', import.meta.url)), base: './', plugins: [svelte()], server: { port: 1420, strictPort: true }, build: { target: 'esnext' } })
