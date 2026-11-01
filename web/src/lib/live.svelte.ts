// The live data every page reads: the latest snapshot from GET /api/stream,
// which the hub sends every few seconds over one connection. The shell
// connects while someone who can view Bield has it open, and a tab that stays
// hidden closes the connection until it's shown again.

import type { Snapshot } from './api/types.js';
import { backoff } from './backoff.js';

/** The state of the connection to the stream. */
export type Connection = 'connecting' | 'live' | 'reconnecting';

/**
 * How long without a snapshot before a connection counts as dead, whether it
 * looks open or is still connecting: three of the hub's 10-second intervals
 * and a little more.
 */
const staleAfter = 35_000;

/**
 * How long a hidden tab keeps the stream open. Over plain HTTP, a browser
 * opens at most six connections to Bield, across all its tabs, and each open
 * stream holds one.
 */
const hiddenFor = 30_000;

/** Where the browser keeps Bield's name, so a reload shows it before the first snapshot. */
const nameKey = 'bield:name';

function storedName(): string {
	try {
		return localStorage.getItem(nameKey) ?? '';
	} catch {
		// Storage is off, as in some private windows.
		return '';
	}
}

function storeName(name: string) {
	try {
		localStorage.setItem(nameKey, name);
	} catch {
		// Not worth telling anyone: the name just shows late next time.
	}
}

class Live {
	/** The latest snapshot, or null before the first. */
	snapshot: Snapshot | null = $state(null);
	connection: Connection = $state('connecting');
	/** When the latest snapshot arrived, in Unix milliseconds, or 0. */
	receivedAt = $state(0);
	/** The name an admin gave this Bield in the settings. */
	#stored = $state(storedName());

	/**
	 * What Bield is called: the latest snapshot's name, else the last one this
	 * browser saw, else "Bield".
	 */
	get name(): string {
		return this.snapshot?.hub.name || this.#stored || 'Bield';
	}

	/** Whether this Bield runs a demo, with made-up machines. */
	get demo(): boolean {
		return this.snapshot?.hub.demo === true;
	}

	/** Takes a snapshot as the latest. */
	receive(snapshot: Snapshot) {
		this.snapshot = snapshot;
		this.receivedAt = Date.now();
		this.connection = 'live';
		if (snapshot.hub.name && snapshot.hub.name !== this.#stored) {
			this.#stored = snapshot.hub.name;
			storeName(snapshot.hub.name);
		}
	}
}

/** What the hub last sent, and whether it's still connected. */
export const live = new Live();

interface Options {
	/**
	 * Called each time the connection drops, before Bield waits to
	 * reconnect, and when a tab that was hidden long enough to close it is
	 * shown again. The hub also ends the stream when the caller's role
	 * changes, so this is when to ask again who they are.
	 */
	onDrop?: () => void;
}

/**
 * Connects to the stream and keeps it connected, until the function it
 * returns is called.
 */
export function connect({ onDrop }: Options = {}): () => void {
	let source: EventSource | null = null;
	let retry: ReturnType<typeof setTimeout> | undefined;
	let hiding: ReturnType<typeof setTimeout> | undefined;
	let failures = 0;
	/** When the current connection was opened, in Unix milliseconds. */
	let openedAt = 0;
	/** Whether the stream is closed because the tab is hidden. */
	let asleep = false;

	function open() {
		clearTimeout(retry);
		source?.close();
		source = new EventSource('/api/stream');
		openedAt = Date.now();
		source.addEventListener('snapshot', (e) => {
			live.receive(JSON.parse(e.data));
			failures = 0;
		});
		// EventSource retries some failures by itself, at its own pace, and
		// gives up on others, such as a 403. Bield retries all of them.
		source.onerror = drop;
	}

	function drop() {
		source?.close();
		source = null;
		live.connection = 'reconnecting';
		clearTimeout(retry);
		retry = setTimeout(open, backoff(failures++));
		onDrop?.();
	}

	/** Reconnects at once if the stream isn't live, such as when a phone wakes the tab. */
	function revive() {
		if (document.visibilityState === 'visible' && live.connection !== 'live') {
			failures = 0;
			open();
		}
	}

	/** Closes the stream until the tab is shown again. */
	function sleep() {
		clearTimeout(retry);
		source?.close();
		source = null;
		asleep = true;
		live.connection = 'connecting';
	}

	function onVisibilityChange() {
		clearTimeout(hiding);
		if (document.visibilityState === 'hidden') {
			hiding = setTimeout(sleep, hiddenFor);
			return;
		}
		if (asleep) {
			asleep = false;
			// The caller's role may have changed while the stream was closed.
			onDrop?.();
		}
		revive();
	}

	// A connection can die without an error, or hang while it connects, such
	// as when the network changes under a sleeping laptop.
	const watchdog = setInterval(() => {
		if (source && Date.now() - Math.max(openedAt, live.receivedAt) > staleAfter) {
			drop();
		}
	}, 5_000);

	live.connection = 'connecting';
	open();
	if (document.visibilityState === 'hidden') {
		hiding = setTimeout(sleep, hiddenFor);
	}
	document.addEventListener('visibilitychange', onVisibilityChange);
	window.addEventListener('online', revive);

	return () => {
		clearTimeout(retry);
		clearTimeout(hiding);
		clearInterval(watchdog);
		document.removeEventListener('visibilitychange', onVisibilityChange);
		window.removeEventListener('online', revive);
		source?.close();
		source = null;
	};
}
