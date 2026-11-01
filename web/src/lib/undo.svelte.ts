// Deleting with undo, for what loses data if it's deleted and brought back
// (a check's history, a channel's secrets). The row goes at once and a toast
// offers "Undo" for a few seconds; the delete is sent when the toast runs out.
// Where undoing just does the opposite, such as starting a container that was
// stopped, the toast's action does that instead (see Docker's ActionDialog).
//
// A tab that closes in that window sends the delete as it goes, which the
// browser may drop: then nothing was deleted, and the row is still there.

import { SvelteSet } from 'svelte/reactivity';
import { toast } from './toast.svelte.js';
import { DeferQueue } from './undo.js';

/** How long someone has to undo, in milliseconds. */
const undoWindow = 6000;

/**
 * How long a deleted row stays hidden after the hub said yes, so it doesn't
 * come back for the moment before the next snapshot leaves it out.
 */
const settle = 8000;

const hidden = new SvelteSet<string>();
const queue = new DeferQueue();

/** Whether something was deleted, or is waiting to be, so a list leaves it out. */
export function isHidden(key: string): boolean {
	return hidden.has(key);
}

export interface Deletion {
	/** Tells the thing apart from everything else, such as `check:<id>`. */
	key: string;
	/** What the toast says, such as "Deleted Homepage". */
	message: string;
	/** Sends the delete. If it throws, the thing comes back (the client has shown why). */
	commit: () => Promise<unknown>;
}

/** Hides a thing, and deletes it unless it's undone in time. */
export function deleteWithUndo({ key, message, commit }: Deletion): void {
	hidden.add(key);
	queue.add(
		key,
		async () => {
			try {
				await commit();
				setTimeout(() => hidden.delete(key), settle);
			} catch {
				hidden.delete(key);
			}
		},
		undoWindow
	);
	toast(message, 'default', {
		duration: undoWindow,
		action: {
			label: 'Undo',
			run: () => {
				if (queue.cancel(key)) {
					hidden.delete(key);
				}
			}
		}
	});
}

if (typeof addEventListener === 'function') {
	addEventListener('pagehide', () => queue.flush());
}
