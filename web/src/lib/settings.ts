// What the Settings page needs to know about the settings: how to name and
// bound them, and when a form has changed. The hub checks everything again;
// these only say so sooner.

import type { AlertRules, InstalledService, RetentionSettings, Thresholds } from './api/types.js';

/** One alert rule: its key in AlertRules, its name, and what it alerts about. */
export interface RuleInfo {
	key: keyof AlertRules;
	label: string;
	description: string;
}

/** The alert rules, in the order the page lists them. */
export const rules: RuleInfo[] = [
	{ key: 'offline', label: 'Device offline', description: 'A device has left the tailnet.' },
	{
		key: 'keyExpiry',
		label: 'Key expiring',
		description: "A device's Tailscale key is about to expire."
	},
	{
		key: 'agentStopped',
		label: 'Agent stopped',
		description: "A device's agent has stopped reporting."
	},
	{ key: 'disk', label: 'Disk full', description: "A device's disk is filling up." },
	{ key: 'memory', label: 'Memory', description: 'A device is using most of its memory.' },
	{ key: 'cpu', label: 'CPU', description: 'A device has been busy for a while.' },
	{ key: 'checks', label: 'Checks', description: 'A check keeps failing.' },
	{
		key: 'containers',
		label: 'Containers',
		description: "A Docker container on Roost's machine stopped unexpectedly."
	},
	{
		key: 'units',
		label: 'Systemd units',
		description: "A unit on Roost's machine failed, or a watched one isn't running."
	},
	{
		key: 'collectors',
		label: "Roost's own jobs",
		description: 'Something Roost gathers keeps failing.'
	}
];

/** How many days each kind of history can be kept for, and what it covers. */
export const retention: {
	key: keyof RetentionSettings;
	label: string;
	description: string;
	max: number;
}[] = [
	{
		key: 'activityDays',
		label: 'Activity log',
		description: 'Events, and alerts that resolved with their notes.',
		max: 365
	},
	{
		key: 'historyDays',
		label: 'Device history',
		description: 'What devices used, and round-trip times to them.',
		max: 90
	},
	{
		key: 'checksDays',
		label: 'Check results',
		description: 'Each result of every check, and the health score.',
		max: 90
	}
];

/** Whether two parts of the settings are the same, so a form knows whether to offer Save. */
export function same(a: unknown, b: unknown): boolean {
	return JSON.stringify(a) === JSON.stringify(b);
}

function isRecord(v: unknown): v is Record<string, unknown> {
	return typeof v === 'object' && v !== null && !Array.isArray(v);
}

/**
 * Returns `edited` with each field that is still as it was in `was` taken from
 * `now`, so what an admin edited stays and what they didn't follows the saved
 * settings. It goes into nested objects, and compares anything else whole.
 */
export function follow(edited: unknown, was: unknown, now: unknown): unknown {
	if (isRecord(edited) && isRecord(was) && isRecord(now)) {
		const out: Record<string, unknown> = { ...edited };
		for (const key of Object.keys(now)) {
			out[key] = follow(edited[key], was[key], now[key]);
		}
		return out;
	}
	return same(edited, was) ? now : edited;
}

/**
 * The fields of `edited` that differ from `saved`, to send in a change so that
 * a field nobody edited here keeps what someone else may have saved since.
 */
export function changedFields<T extends object>(edited: T, saved: T): Partial<T> {
	const out: Partial<T> = {};
	for (const key of Object.keys(edited) as (keyof T)[]) {
		if (!same(edited[key], saved[key])) {
			out[key] = edited[key];
		}
	}
	return out;
}

/**
 * Reads a number field's value, which is a number or null when the field is
 * empty or isn't a number, as `bind:value` on `type="number"` gives it.
 */
export function numberOf(v: number | string | null | undefined): number | undefined {
	if (v == null || v === '') {
		return undefined;
	}
	const n = typeof v === 'number' ? v : Number(v);
	return Number.isFinite(n) ? n : undefined;
}

/** What is wrong with thresholds, in a sentence, or '' if nothing is. */
export function thresholdsProblem(t: Thresholds): string {
	const percents: [string, number][] = [
		['disk warning', t.diskWarn],
		['critical disk', t.diskCritical],
		['memory', t.memoryWarn],
		['CPU', t.cpuWarn]
	];
	for (const [name, v] of percents) {
		if (!(v > 0 && v <= 100)) {
			return `The ${name} threshold must be a percentage above 0.`;
		}
	}
	if (t.diskCritical < t.diskWarn) {
		return "A disk can't be critical before it needs a warning.";
	}
	const whole = (n: number, min: number, max: number) =>
		Number.isInteger(n) && n >= min && n <= max;
	if (!whole(t.cpuWindowMinutes, 1, 60)) {
		return 'The time the CPU must stay busy must be from a minute to an hour.';
	}
	if (!whole(t.offlineMinutes, 1, 7 * 24 * 60)) {
		return 'The time a device can be offline must be from a minute to a week.';
	}
	if (!whole(t.keyExpiryDays, 1, 365)) {
		return 'The warning before a key expires must be from a day to a year.';
	}
	if (!whole(t.checkFailures, 1, 100)) {
		return 'The failures in a row before an alert must be from 1 to 100.';
	}
	return '';
}

/** What is wrong with how long to keep history, in a sentence, or '' if nothing is. */
export function retentionProblem(r: RetentionSettings): string {
	for (const info of retention) {
		const days = r[info.key];
		if (!Number.isInteger(days) || days < 1 || days > info.max) {
			return `Keep the ${info.label.toLowerCase()} for 1 to ${info.max} days.`;
		}
	}
	return '';
}

/** Whether any kind of history would be kept for fewer days, so older data would go. */
export function shortensRetention(from: RetentionSettings, to: RetentionSettings): boolean {
	return retention.some(({ key }) => to[key] < from[key]);
}

/** A watched unit's name without its scope, and whether it is the user's. */
export function parseUnit(key: string): { name: string; user: boolean } {
	return key.startsWith('user:')
		? { name: key.slice('user:'.length), user: true }
		: { name: key, user: false };
}

/** The key the settings name a unit by, such as "ssh.service" or "user:app.service". */
export function unitKey(name: string, user: boolean): string {
	return user ? `user:${name}` : name;
}

/**
 * The services whose name or description has every word of the query,
 * ignoring case. An empty query matches all of them.
 */
export function matchServices(services: InstalledService[], query: string): InstalledService[] {
	const words = query.toLowerCase().split(/\s+/).filter(Boolean);
	if (words.length === 0) {
		return services;
	}
	return services.filter((s) => {
		const text =
			`${s.name} ${s.description ?? ''} ${s.scope === 'user' ? 'user' : ''}`.toLowerCase();
		return words.every((w) => text.includes(w));
	});
}

/** A DNS name: labels of letters, digits and hyphens, none starting or ending with a hyphen. */
const dnsName =
	/^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$/;

/** Whether a host, without brackets, is a DNS name or an IP address without a zone. */
function validHost(host: string): boolean {
	if (host.includes(':')) {
		// An IPv6 address. The URL parser refuses one with a zone.
		return URL.canParse(`http://[${host}]`);
	}
	return host.length <= 253 && dnsName.test(host);
}

/**
 * What is wrong with a public URL, in a sentence, or '' if nothing is. It
 * can be empty, or http(s)://host with an optional port: no path, query,
 * fragment or login. The host is a DNS name or an IP address, and the port,
 * if there is one, is from 1 to 65535. A slash at its end is fine, since the
 * hub trims it.
 */
export function publicUrlProblem(url: string): string {
	let s = url.trim();
	if (s.endsWith('/')) {
		s = s.slice(0, -1);
	}
	if (s === '') {
		return '';
	}
	const webAddress = 'The public URL must be a web address, such as https://roost.example.ts.net.';
	const authority = /^https?:\/\/([\s\S]+)$/i.exec(s)?.[1];
	if (authority === undefined) {
		return webAddress;
	}
	if (/[\s\u0000-\u001f\u007f]/.test(s)) {
		return "The public URL can't have spaces in it.";
	}
	if (/[/\\?#@]/.test(authority)) {
		return 'The public URL can only be a scheme, a host and a port, such as https://roost.example.ts.net:8443.';
	}
	// A host in brackets, such as [fd7a::1], ends at the bracket. Any other host
	// ends at the last colon, which starts the port.
	let host = authority;
	let port: string | undefined;
	if (authority.startsWith('[')) {
		const end = authority.indexOf(']');
		const after = end < 0 ? '' : authority.slice(end + 1);
		if (end < 0 || (after !== '' && !after.startsWith(':'))) {
			return webAddress;
		}
		host = authority.slice(1, end);
		port = after === '' ? undefined : after.slice(1);
	} else if (authority.includes(':')) {
		const colon = authority.lastIndexOf(':');
		host = authority.slice(0, colon);
		port = authority.slice(colon + 1);
	}
	if (
		!validHost(host) ||
		(port !== undefined && !(/^[0-9]+$/.test(port) && Number(port) >= 1 && Number(port) <= 65535))
	) {
		return "The public URL's host must be a name or an IP address, and its port a number from 1 to 65535.";
	}
	return '';
}

/** Whether two lists hold the same strings, in any order. */
export function sameSet(a: readonly string[], b: readonly string[]): boolean {
	const x = new Set(a);
	const y = new Set(b);
	return x.size === y.size && [...x].every((k) => y.has(k));
}

/**
 * Reads a unit name typed into the add form: it's trimmed, and gets
 * ".service" if it has no dot. The name is empty if nothing was typed, and
 * the problem says in a sentence what is wrong with a name, or is '' if
 * nothing is. The hub holds names to what systemd does: letters, digits and
 * ": _ . \ @ -", at most 255 of them, no dash first, and no template such as
 * "getty@.service".
 */
export function readUnitName(input: string): { name: string; problem: string } {
	const typed = input.trim();
	if (typed === '') {
		return { name: '', problem: '' };
	}
	const bad = (problem: string) => ({ name: typed, problem });
	if (typed.startsWith('-')) {
		return bad("A unit name can't start with a dash.");
	}
	if (typed.startsWith('user:')) {
		return bad('Pick User from the list instead of typing user:.');
	}
	if (!/^[A-Za-z0-9:_.\\@-]+$/.test(typed)) {
		return bad(
			'A unit name can only have letters, digits and the characters : _ . \\ @ - (so no spaces or slashes).'
		);
	}
	const name = typed.includes('.') ? typed : `${typed}.service`;
	if (name.includes('@.')) {
		return bad(
			"A template such as getty@.service can't be watched. Name an instance, such as getty@tty1.service."
		);
	}
	if (name.length > 255) {
		return bad('A unit name can have at most 255 characters.');
	}
	return { name, problem: '' };
}

/**
 * What is wrong with a login typed into the add form, in a sentence, or ''
 * if nothing is. Tailscale logins are email addresses, but Headscale names a
 * user without an email by name, so a login is just one word.
 */
export function loginProblem(login: string): string {
	const s = login.trim();
	if (s === '' || /\s/.test(s)) {
		return 'Use a login without spaces, such as alice@example.com (or alice on Headscale).';
	}
	return '';
}
