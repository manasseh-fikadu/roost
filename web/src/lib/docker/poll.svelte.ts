// Asking again every few seconds, for a page that shows what's happening now.

/**
 * Loads something now and then every `interval` milliseconds while the tab is
 * shown, and keeps the latest answer. If a load fails, the answer before it
 * stays, and `error` says why, so a page can show what it had and that it's
 * stale. Make one for each thing a page shows, and call `start` in an
 * `$effect`, which gives back what stops it.
 */
export class Poll<T> {
	/** The latest answer, or null before the first. */
	data = $state<T | null>(null);
	/** Why the latest load failed, or null if it worked. */
	error = $state<string | null>(null);
	/** When the latest load worked, in Unix milliseconds, or 0. */
	at = $state(0);
	/** Whether the first load is still on its way. */
	loading = $state(true);

	#load: (signal: AbortSignal) => Promise<T>;
	#interval: number;
	#reload: (() => Promise<void>) | undefined;

	constructor(load: (signal: AbortSignal) => Promise<T>, interval: number) {
		this.#load = load;
		this.#interval = interval;
	}

	/** Loads again now, if the poll is running. */
	reload(): Promise<void> {
		return this.#reload?.() ?? Promise.resolve();
	}

	/** Starts loading. It returns the function that stops. */
	start(): () => void {
		const controller = new AbortController();
		let timer: ReturnType<typeof setTimeout> | undefined;
		let running = false;
		let again = false;
		const load = async () => {
			if (running) {
				// A load is on its way, and this one asks for what it may have missed.
				again = true;
				return;
			}
			running = true;
			try {
				const value = await this.#load(controller.signal);
				if (!controller.signal.aborted) {
					this.data = value;
					this.error = null;
					this.at = Date.now();
				}
			} catch (err) {
				if (!controller.signal.aborted) {
					this.error = err instanceof Error ? err.message : String(err);
				}
			} finally {
				running = false;
				if (!controller.signal.aborted) {
					this.loading = false;
					clearTimeout(timer);
					timer = setTimeout(tick, again ? 0 : this.#interval);
					again = false;
				}
			}
		};
		const tick = () => {
			// A tab nobody sees waits to be shown again.
			if (document.visibilityState === 'hidden') {
				timer = setTimeout(tick, this.#interval);
				return;
			}
			void load();
		};
		const onShow = () => {
			if (document.visibilityState === 'visible') {
				clearTimeout(timer);
				void load();
			}
		};
		this.#reload = load;
		document.addEventListener('visibilitychange', onShow);
		void load();
		return () => {
			controller.abort();
			clearTimeout(timer);
			document.removeEventListener('visibilitychange', onShow);
			this.#reload = undefined;
		};
	}
}
