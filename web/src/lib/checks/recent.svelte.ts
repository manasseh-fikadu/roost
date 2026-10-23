// The latest results of every check, which the Checks page draws as bars.
// Each snapshot has only each check's last result, so the page loads the
// ones before once, and then adds each new one as it comes.

import { untrack } from 'svelte';
import { getRecentResults } from '#lib/api/client.js';
import type { Check } from '#lib/api/types.js';
import { addLatest, type ResultsByCheck } from '#lib/checks.js';

export class RecentResults {
	/** Each check's latest results, oldest first, by check ID. */
	byId: ResultsByCheck = $state.raw({});

	/**
	 * Loads every check's latest results from the hub, and adds the last
	 * results of `checks`, which may be newer. If it fails, it keeps what it
	 * has.
	 */
	async load(checks: () => Check[]): Promise<void> {
		try {
			const r = await getRecentResults();
			this.byId = addLatest(r.checks, checks());
		} catch {
			// The bars fill in from the snapshots that come.
		}
	}

	/** Adds the last result of each check, if it's new. */
	observe(checks: Check[]): void {
		const byId = untrack(() => this.byId);
		const next = addLatest(byId, checks);
		if (next !== byId) {
			this.byId = next;
		}
	}
}
