// Deleting with a way back: the thing is hidden at once, a toast offers
// "Undo", and the request that deletes it waits until the toast runs out. The
// queue is the waiting; undo.svelte.ts hides things and shows the toast.

/** Runs `fn` after `ms` milliseconds, and returns what cancels it. */
export type Schedule = (fn: () => void, ms: number) => () => void;

const setTimer: Schedule = (fn, ms) => {
	const timer = setTimeout(fn, ms);
	return () => clearTimeout(timer);
};

/** Things waiting to be done, by key, each of which can be taken back until its time is up. */
export class DeferQueue {
	#waiting = new Map<string, { cancel: () => void; run: () => void }>();
	#schedule: Schedule;

	constructor(schedule: Schedule = setTimer) {
		this.#schedule = schedule;
	}

	/** Whether something is waiting under `key`. */
	has(key: string): boolean {
		return this.#waiting.has(key);
	}

	/**
	 * Does `run` after `ms`, unless `cancel(key)` comes first. Adding under a
	 * key that's waiting does the earlier one now: it was decided already.
	 */
	add(key: string, run: () => void, ms: number): void {
		this.flushKey(key);
		const entry = {
			run,
			cancel: this.#schedule(() => {
				this.#waiting.delete(key);
				run();
			}, ms)
		};
		this.#waiting.set(key, entry);
	}

	/** Takes back what waits under `key`. It says whether anything did. */
	cancel(key: string): boolean {
		const entry = this.#waiting.get(key);
		if (!entry) {
			return false;
		}
		entry.cancel();
		this.#waiting.delete(key);
		return true;
	}

	/** Does what waits under `key` now. */
	flushKey(key: string): void {
		const entry = this.#waiting.get(key);
		if (entry) {
			entry.cancel();
			this.#waiting.delete(key);
			entry.run();
		}
	}

	/** Does everything that waits now, such as when the page is closing. */
	flush(): void {
		for (const key of [...this.#waiting.keys()]) {
			this.flushKey(key);
		}
	}
}
