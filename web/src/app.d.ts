// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
import type { Me } from '#lib/api/types.js';

declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		/** What the root layout loads for every page (src/routes/+layout.ts). */
		interface PageData {
			/** Who's asking, from GET /api/me, or null if the hub didn't answer. */
			me: Me | null;
			/** Why GET /api/me failed, or null if it didn't. */
			hubError: string | null;
		}
		// interface PageState {}
		// interface Platform {}
	}
}

export {};
