/** The first wait before trying again, in milliseconds. It doubles each time. */
const firstDelay = 1_000;
/** The longest wait before trying again. */
const maxDelay = 30_000;

/**
 * How long to wait before trying again after attempt failures in a row: a
 * random time between half and all of a delay that doubles each attempt, up
 * to 30 seconds, so that every open tab doesn't reconnect at once after the
 * hub restarts.
 */
export function backoff(attempt: number, random: () => number = Math.random): number {
	const ceiling = Math.min(maxDelay, firstDelay * 2 ** attempt);
	return ceiling / 2 + (random() * ceiling) / 2;
}
