// A container's live stats: the latest few minutes, and a sample each second,
// from one stream that everyone watching the container shares.

import type { DockerStats } from '#lib/api/types.js';
import { backoff } from '#lib/backoff.js';
import { dockerStatsUrl } from './api.js';

/** How many samples are kept: five minutes. */
export const keptSamples = 300;

/**
 * How many times in a row the stream fails to open before it is given up on:
 * a container that was removed, or a daemon that is gone, won't be back soon,
 * and the page says so elsewhere.
 */
const maxFailures = 6;

/** Streams a container's stats. Make one, and call `connect` in an `$effect`. */
export class LiveStats {
	/** The latest samples, oldest first. */
	samples = $state<DockerStats[]>([]);
	/** Why the stats ended, such as "The container stopped", or null while they go on. */
	ended = $state<string | null>(null);
	/** Whether the stream is open. */
	live = $state(false);

	/**
	 * Starts streaming the stats of a container, which clears what there was.
	 * With no `containerId`, or if `enabled` is false, it only clears. It
	 * returns the function that stops.
	 */
	connect(deviceId: string, containerId: string | null, enabled = true): () => void {
		this.samples = [];
		this.ended = null;
		this.live = false;
		if (!containerId || !enabled) {
			return () => {};
		}
		let source: EventSource | undefined;
		let retry: ReturnType<typeof setTimeout> | undefined;
		let failures = 0;
		let stopped = false;
		const open = () => {
			source = new EventSource(dockerStatsUrl(deviceId, containerId));
			source.addEventListener('backlog', (e) => {
				this.samples = JSON.parse(e.data);
				this.live = true;
				failures = 0;
			});
			source.addEventListener('sample', (e) => {
				const next: DockerStats = JSON.parse(e.data);
				this.samples =
					this.samples.length >= keptSamples
						? [...this.samples.slice(1), next]
						: [...this.samples, next];
			});
			source.addEventListener('end', (e) => {
				this.ended = JSON.parse(e.data).reason;
				this.live = false;
				// The stream is over. EventSource would open it again.
				source?.close();
			});
			// The connection broke: the hub ended it because the caller's role
			// changed, or the network dropped.
			source.onerror = () => {
				source?.close();
				this.live = false;
				if (stopped || this.ended !== null) {
					return;
				}
				if (failures >= maxFailures) {
					this.ended = "Roost couldn't keep the stats stream open";
					return;
				}
				retry = setTimeout(open, backoff(failures++));
			};
		};
		open();
		return () => {
			stopped = true;
			clearTimeout(retry);
			source?.close();
			this.live = false;
		};
	}
}
