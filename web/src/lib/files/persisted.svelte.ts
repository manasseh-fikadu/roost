// A preference kept in localStorage, which the page reads and writes as a plain value.

/** A value kept in localStorage under a key, and in memory while the page is open. */
export class Persisted<T> {
	#value = $state() as T;
	readonly #key: string;

	constructor(key: string, initial: T) {
		this.#key = key;
		this.#value = initial;
		try {
			const raw = localStorage.getItem(key);
			if (raw !== null) {
				this.#value = JSON.parse(raw) as T;
			}
		} catch {
			// Private mode, or something else that isn't JSON: use the default.
		}
	}

	get value(): T {
		return this.#value;
	}

	set value(v: T) {
		this.#value = v;
		try {
			localStorage.setItem(this.#key, JSON.stringify(v));
		} catch {
			// The preference won't stick, which is no reason to fail.
		}
	}
}
