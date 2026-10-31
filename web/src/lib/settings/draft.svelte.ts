// A form's copy of a part of the settings, to edit before saving.

import { untrack } from 'svelte';
import { follow, same } from '#lib/settings.js';

/** Copies a value that has no functions or cycles, such as a part of the settings. */
function clone<T>(v: T): T {
	return structuredClone($state.snapshot(v)) as T;
}

/**
 * The draft of a part of the settings a form edits. It starts as a copy of the
 * saved part. When the saved part changes, because the form was saved or
 * someone else changed it, the draft follows it, except for the fields that
 * have been edited, which stay, so a half-edited form isn't wiped and saving
 * it doesn't undo what someone else changed.
 */
export class Draft<T> {
	/** What the form edits. */
	value: T = $state() as T;
	/** The saved part, as last seen. */
	base: T = $state.raw() as T;
	readonly #equal: (a: T, b: T) => boolean;

	/**
	 * `source` returns the saved part, such as from a prop, and is read again
	 * whenever it changes. `equal` says whether two copies are the same, for
	 * parts that `same` can't compare, such as a list whose order doesn't matter.
	 */
	constructor(source: () => T, equal: (a: T, b: T) => boolean = same) {
		this.#equal = equal;
		this.base = clone(untrack(source));
		this.value = clone(this.base);
		$effect.pre(() => {
			const next = source();
			untrack(() => {
				if (this.#equal(next, this.base)) {
					return;
				}
				this.value = this.#equal(this.value, this.base)
					? clone(next)
					: (follow(clone(this.value), this.base, clone(next)) as T);
				this.base = clone(next);
			});
		});
	}

	/** Whether the draft differs from the saved part. */
	get dirty(): boolean {
		return !this.#equal(this.value, this.base);
	}

	/** Drops the edits. */
	reset() {
		this.value = clone(this.base);
	}

	/** Takes the part that was just saved as both the saved part and the draft. */
	adopt(saved: T) {
		this.base = clone(saved);
		this.value = clone(saved);
	}
}
