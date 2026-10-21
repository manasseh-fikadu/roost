// Formats numbers and times for people.

/** A time of day to the second, such as 9:47:11 PM. */
export function clock(ts: number): string {
	return new Date(ts).toLocaleTimeString(undefined, {
		hour: 'numeric',
		minute: '2-digit',
		second: '2-digit'
	});
}

/** A count and a noun that agrees with it, such as "1 device" or "3 devices". */
export function plural(n: number, word: string, pluralWord = `${word}s`): string {
	return `${n} ${n === 1 ? word : pluralWord}`;
}
