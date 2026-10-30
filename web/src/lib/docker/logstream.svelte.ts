// A container's logs as they stream in: the lines the viewer keeps, and
// whether the stream is open. Make one, and call `connect` in an `$effect`.

import { untrack } from 'svelte';
import type { DockerLogLine, DockerStreamEnd, DockerStreamFailure } from '#lib/api/types.js';
import { backoff } from '#lib/backoff.js';
import { dockerLogsUrl, type LogQuery } from './api.js';
import {
	appendLines,
	makeEntry,
	maxLines,
	recentCounts,
	resumePoint,
	takeRepeat,
	timeKey,
	type LogEntry
} from './logs.js';

/**
 * What the stream is doing: connecting, loading the lines of a container that
 * isn't running, live on one that is, waiting to try again after the
 * connection broke, ended as it should, or failed.
 */
export type StreamStatus = 'connecting' | 'loading' | 'live' | 'reconnecting' | 'ended' | 'failed';

/** What to read, which changes when the viewer's filters do. */
export interface StreamQuery {
	/** How many lines from the end to start with. */
	tail: number;
	/** An age such as 15m, or empty for no limit. */
	since: string;
	stdout: boolean;
	stderr: boolean;
}

/** How long lines wait to be shown, so a fast stream redraws a few times a second and not hundreds. */
const flushDelay = 60;
/** How many times in a row the connection may break before the stream gives up. */
const maxAttempts = 8;

export class LogStream {
	/** The lines, oldest first. It is a new array each time lines arrive. */
	lines = $state.raw<LogEntry[]>([]);
	status = $state<StreamStatus>('connecting');
	/** Why the stream ended or failed, or what it is doing about a broken connection. */
	message = $state<string | null>(null);
	/** How many lines have arrived since `connect`, including the ones that were let go. */
	received = $state(0);

	#device = '';
	#container = '';
	#query: StreamQuery = { tail: 0, since: '', stdout: true, stderr: true };
	#follow = false;
	#source: EventSource | undefined;
	#flushTimer: ReturnType<typeof setTimeout> | undefined;
	#retryTimer: ReturnType<typeof setTimeout> | undefined;
	#pending: LogEntry[] = [];
	#nextN = 1;
	#attempts = 0;
	/** The time of the newest line, as Docker wrote it, and its key. */
	#newest: string | null = null;
	#newestKey = '';
	/** The lines that a stream picked up again may send a second time. */
	#seen: Map<string, number> | null = null;
	/** Lines at or before this time were cleared, and are not shown again. */
	#floorKey = '';

	/**
	 * Starts streaming the logs of a container, which clears the lines there
	 * were. `follow` keeps the stream open for new lines. It returns the
	 * function that stops.
	 */
	connect(device: string, container: string, query: StreamQuery, follow: boolean): () => void {
		return untrack(() => {
			this.#close();
			this.#device = device;
			this.#container = container;
			this.#query = query;
			this.#follow = follow;
			this.lines = [];
			this.received = 0;
			this.message = null;
			this.#pending = [];
			this.#nextN = 1;
			this.#attempts = 0;
			this.#newest = null;
			this.#newestKey = '';
			this.#floorKey = '';
			this.#open(false);
			return () => this.#close();
		});
	}

	/**
	 * Opens the stream again where it left off, keeping the lines, such as
	 * after it ended and the container started again.
	 */
	resume(follow: boolean) {
		untrack(() => {
			clearTimeout(this.#retryTimer);
			this.#follow = follow;
			this.#attempts = 0;
			this.#open(true);
		});
	}

	/** Clears the lines. The stream goes on, and doesn't bring them back. */
	clear() {
		untrack(() => {
			this.#pending = [];
			this.#floorKey = this.#newestKey;
			this.lines = [];
		});
	}

	#close() {
		this.#source?.close();
		this.#source = undefined;
		clearTimeout(this.#flushTimer);
		clearTimeout(this.#retryTimer);
		this.#flushTimer = undefined;
		this.#retryTimer = undefined;
	}

	#open(resuming: boolean) {
		this.#source?.close();
		this.#flush();
		const q: LogQuery = {
			follow: this.#follow,
			tail: this.#query.tail,
			since: this.#query.since || undefined,
			stdout: this.#query.stdout,
			stderr: this.#query.stderr
		};
		this.#seen = null;
		const point = resuming && this.#newest ? resumePoint(this.#newest) : null;
		if (point) {
			q.tail = 'all';
			q.since = point.since;
			this.#seen = recentCounts(this.lines, point.key);
		} else if (resuming && this.lines.length > 0) {
			// The lines have no times to pick up from, and asking for them again
			// would show them twice: take only what comes next.
			q.tail = 0;
		}
		this.status = 'connecting';
		this.message = null;
		const source = new EventSource(dockerLogsUrl(this.#device, this.#container, q));
		this.#source = source;
		source.addEventListener('start', () => {
			if (this.#source !== source) return;
			this.#attempts = 0;
			this.status = this.#follow ? 'live' : 'loading';
		});
		source.addEventListener('lines', (e) => {
			if (this.#source !== source) return;
			let lines: DockerLogLine[];
			try {
				lines = JSON.parse(e.data);
			} catch {
				return;
			}
			if (Array.isArray(lines)) {
				this.#receive(lines);
			}
		});
		source.addEventListener('end', (e) => {
			if (this.#source !== source) return;
			// The stream is over. EventSource would open it again.
			source.close();
			this.#flush();
			this.status = 'ended';
			this.message = reason<DockerStreamEnd>(e.data, 'reason') ?? 'The log ended';
		});
		source.addEventListener('failure', (e) => {
			if (this.#source !== source) return;
			source.close();
			this.#flush();
			this.status = 'failed';
			this.message = reason<DockerStreamFailure>(e.data, 'message') ?? 'The log failed';
		});
		// The connection broke: the network dropped, or Roost restarted, or
		// Roost answered with an error, which an EventSource can't read.
		source.onerror = () => {
			if (this.#source !== source) return;
			source.close();
			this.#flush();
			this.status = 'reconnecting';
			this.message = 'Lost the connection. Trying again…';
			void this.#whyNot().then((why) => {
				if (this.#source !== source) return;
				if (why !== null) {
					// Asking again won't help.
					this.status = 'failed';
					this.message = why;
					return;
				}
				if (++this.#attempts > maxAttempts) {
					this.status = 'failed';
					this.message = 'Lost the connection to Roost.';
					return;
				}
				this.#retryTimer = setTimeout(() => this.#open(true), backoff(this.#attempts - 1));
			});
		};
	}

	/**
	 * Asks for no lines of the log, to learn why the stream broke: Roost's
	 * answer to an error is a message, and an EventSource only says it
	 * failed. It returns the message, or null if the request worked or never
	 * got to Roost, which is a break that asking again may mend.
	 */
	async #whyNot(): Promise<string | null> {
		try {
			const res = await fetch(
				dockerLogsUrl(this.#device, this.#container, { follow: false, tail: 0 }),
				{ headers: { accept: 'application/json' } }
			);
			if (res.ok) {
				await res.body?.cancel();
				return null;
			}
			const body: { error?: string } = await res.json().catch(() => ({}));
			// A hub that is starting or stopping answers 502 to a proxy, with no message.
			return body.error ?? null;
		} catch {
			return null;
		}
	}

	#receive(lines: DockerLogLine[]) {
		for (const l of lines) {
			const key = l.t ? timeKey(l.t) : '';
			if (key !== '' && key <= this.#floorKey) {
				continue;
			}
			if (this.#seen && takeRepeat(this.#seen, l)) {
				continue;
			}
			if (key > this.#newestKey) {
				this.#newestKey = key;
				this.#newest = l.t ?? null;
			}
			this.#pending.push(makeEntry(this.#nextN++, l));
		}
		if (this.#pending.length > maxLines) {
			this.#pending.splice(0, this.#pending.length - maxLines);
		}
		this.#flushTimer ??= setTimeout(() => this.#flush(), flushDelay);
	}

	#flush() {
		clearTimeout(this.#flushTimer);
		this.#flushTimer = undefined;
		if (this.#pending.length === 0) {
			return;
		}
		const batch = this.#pending;
		this.#pending = [];
		this.lines = appendLines(this.lines, batch, maxLines);
		this.received += batch.length;
	}
}

/** One string out of the JSON of an event, or null. */
function reason<T extends object>(data: string, key: keyof T & string): string | null {
	try {
		const v = (JSON.parse(data) as Record<string, unknown>)[key];
		return typeof v === 'string' && v !== '' ? v : null;
	} catch {
		return null;
	}
}
