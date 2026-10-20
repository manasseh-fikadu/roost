import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig, loadEnv } from 'vite';

export default defineConfig(({ mode }) => {
	// ROOST_API is the Go server that `make dev` starts next to Vite.
	const env = loadEnv(mode, '.', 'ROOST_');

	return {
		plugins: [
			tailwindcss(),
			sveltekit({
				compilerOptions: {
					// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
					runes: ({ filename }) =>
						filename.split(/[/\\]/).includes('node_modules') ? undefined : true
				},
				// A single-page app that the Go binary embeds and serves. The Go side
				// (internal/api/ui.go) serves 200.html for every path that isn't a file.
				adapter: adapter({ fallback: '200.html' })
			})
		],
		server: {
			proxy: {
				'/api': env.ROOST_API || 'http://127.0.0.1:4700'
			}
		}
	};
});
