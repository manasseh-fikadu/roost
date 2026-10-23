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

/** What a number that's missing or isn't one shows as. */
const none = '—';

function finite(n: number | null | undefined): n is number {
	return n != null && Number.isFinite(n);
}

/** A number of bytes, such as 512 B, 1.5 KB or 120 GB. */
export function bytes(n: number | null | undefined, digits = 1): string {
	if (!finite(n)) {
		return none;
	}
	const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
	let v = n;
	let i = 0;
	while (Math.abs(v) >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v.toFixed(i === 0 || Math.abs(v) >= 100 ? 0 : digits)} ${units[i]}`;
}

/** Bytes per second, such as 300 KB/s. */
export function rate(n: number | null | undefined): string {
	return finite(n) ? `${bytes(n)}/s` : none;
}

/** Bytes per second in a few characters, such as 402B, 9.1K or 2.3M. */
export function rateShort(n: number | null | undefined): string {
	if (!finite(n)) {
		return none;
	}
	const units = ['B', 'K', 'M', 'G', 'T'];
	let v = n;
	let i = 0;
	while (v >= 1024 && i < units.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v.toFixed(i === 0 || v >= 100 ? 0 : 1)}${units[i]}`;
}

/** A percentage, such as 34.6%. */
export function percent(n: number | null | undefined, digits = 0): string {
	return finite(n) ? `${n.toFixed(digits)}%` : none;
}

/** How much of a total is used, in percent, or null if it's not known. */
export function ratio(
	used: number | null | undefined,
	total: number | null | undefined
): number | null {
	return finite(used) && finite(total) && total > 0 ? (used / total) * 100 : null;
}

/** A round trip in milliseconds, such as 0.42 ms, 3.8 ms or 80 ms. */
export function latency(ms: number | null | undefined): string {
	if (!finite(ms)) {
		return none;
	}
	if (ms < 1) {
		return `${ms.toFixed(2)} ms`;
	}
	return ms < 10 ? `${ms.toFixed(1)} ms` : `${Math.round(ms)} ms`;
}

/** A span of seconds in its two largest units, such as 3d 10h, 2h 5m or 40s. */
export function duration(seconds: number | null | undefined): string {
	if (!finite(seconds)) {
		return none;
	}
	const s = Math.max(0, Math.round(seconds));
	const d = Math.floor(s / 86_400);
	const h = Math.floor((s % 86_400) / 3600);
	const m = Math.floor((s % 3600) / 60);
	if (d) {
		return `${d}d ${h}h`;
	}
	if (h) {
		return `${h}h ${m}m`;
	}
	return m ? `${m}m` : `${s}s`;
}

/** A time in Unix milliseconds, or as an RFC 3339 string. */
type Time = number | string;

function millis(t: Time): number {
	return typeof t === 'string' ? Date.parse(t) : t;
}

/**
 * How long ago a time was, such as "just now", "5m ago" or "3d ago", or how
 * long until it comes, such as "in 2d 3h".
 */
export function ago(t: Time | null | undefined, now = Date.now()): string {
	if (t == null) {
		return 'never';
	}
	const ms = millis(t);
	if (!Number.isFinite(ms)) {
		return 'never';
	}
	const s = Math.round((now - ms) / 1000);
	if (s < 0) {
		return `in ${duration(-s)}`;
	}
	if (s < 5) {
		return 'just now';
	}
	if (s < 60) {
		return `${s}s ago`;
	}
	if (s < 3600) {
		return `${Math.floor(s / 60)}m ago`;
	}
	return s < 86_400 ? `${Math.floor(s / 3600)}h ago` : `${Math.floor(s / 86_400)}d ago`;
}

/** A date, such as Jul 4, 2026. */
export function shortDate(t: Time | null | undefined): string {
	if (t == null) {
		return none;
	}
	return new Date(millis(t)).toLocaleDateString(undefined, {
		month: 'short',
		day: 'numeric',
		year: 'numeric'
	});
}

/** A date and time to the minute, such as Oct 17, 9:36 PM. */
export function dateTime(t: Time | null | undefined): string {
	if (t == null) {
		return none;
	}
	return new Date(millis(t)).toLocaleString(undefined, {
		month: 'short',
		day: 'numeric',
		hour: 'numeric',
		minute: '2-digit'
	});
}

/** A time of day to the minute, such as 9:47 PM. */
export function timeOfDay(t: Time): string {
	return new Date(millis(t)).toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
}

const osNames: Record<string, string> = {
	linux: 'Linux',
	macos: 'macOS',
	windows: 'Windows',
	ios: 'iOS',
	ipados: 'iPadOS',
	tvos: 'tvOS',
	android: 'Android',
	freebsd: 'FreeBSD',
	openbsd: 'OpenBSD',
	illumos: 'illumos'
};

/** An operating system's name, as its makers write it, from Tailscale's name for it. */
export function osLabel(os: string): string {
	return osNames[os.toLowerCase()] ?? (os || 'Unknown OS');
}
