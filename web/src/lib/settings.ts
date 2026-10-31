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
		description: 'What devices and containers used, and round-trip times.',
		max: 30
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

/**
 * What is wrong with a public URL, in a sentence, or '' if nothing is. It
 * can be empty, or http(s)://host with an optional port: no path, query,
 * fragment or login. A slash at its end is fine, since the hub trims it.
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
	try {
		if (new URL(s).hostname === '') {
			return webAddress;
		}
	} catch {
		return webAddress;
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
 * nothing is.
 */
export function readUnitName(input: string): { name: string; problem: string } {
	const typed = input.trim();
	if (typed === '') {
		return { name: '', problem: '' };
	}
	if (/[\s/]/.test(typed)) {
		return { name: typed, problem: "A unit name can't have spaces or slashes in it." };
	}
	if (typed.startsWith('-')) {
		return { name: typed, problem: "A unit name can't start with a dash." };
	}
	if (typed.startsWith('user:')) {
		return { name: typed, problem: 'Pick User from the list instead of typing user:.' };
	}
	return { name: typed.includes('.') ? typed : `${typed}.service`, problem: '' };
}

/**
 * What is wrong with a Tailscale login typed into the add form, in a
 * sentence, or '' if nothing is. A login has one @ with something on each
 * side of it, and no spaces.
 */
export function loginProblem(login: string): string {
	const s = login.trim();
	const at = s.indexOf('@');
	if (s === '' || /\s/.test(s) || at <= 0 || at !== s.lastIndexOf('@') || at === s.length - 1) {
		return 'Use a Tailscale login, such as alice@example.com.';
	}
	return '';
}
