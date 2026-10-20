import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
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
	// `make dev` runs the Go server with --dev-ui, which proxies the web UI to
	// Vite. Open Roost's tailnet URL, not Vite's, so the API sees requests
	// arrive over the tailnet as it does in production.
	server: {
		host: '127.0.0.1',
		strictPort: true
	}
});
