import { getMe } from '#lib/api/client.js';
import type { LayoutLoad } from './$types';

// Bield's UI is a single-page app served by the Go binary, so nothing renders on a server.
export const ssr = false;

// Every page needs to know who's asking, so the root layout asks once, and
// again when the shell invalidates bield:me. When the hub doesn't answer (it
// answers 503 while it starts), the layout says so instead of throwing: an
// error thrown here would skip +error.svelte and show SvelteKit's bare
// fallback page.
export const load: LayoutLoad = async ({ fetch, depends }) => {
	depends('bield:me');
	try {
		return { me: await getMe(fetch), hubError: null };
	} catch (err) {
		return { me: null, hubError: err instanceof Error ? err.message : String(err) };
	}
};
