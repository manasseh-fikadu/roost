// What the container page works out before it shows anything: tabs, times,
// health checks, ports, the rows of the environment and the labels, and
// searching an inspection.

import type {
	ContainerPort,
	ContainerState,
	DockerContainerDetail,
	DockerContainerRow,
	DockerEnvVar,
	DockerHealthResult,
	DockerHealthcheck,
	DockerState
} from '#lib/api/types.js';
import { plural } from '#lib/format.js';
import { argv, cleanExits } from '../docker.js';

/** The page's tabs, in order. */
export const tabValues = [
	'overview',
	'logs',
	'stats',
	'processes',
	'mounts',
	'network',
	'env',
	'labels',
	'inspect'
] as const;

export type Tab = (typeof tabValues)[number];

/** The tab in a URL's ?tab=, or the overview. */
export function parseTab(value: string | null | undefined): Tab {
	return tabValues.find((t) => t === value) ?? 'overview';
}

/**
 * A container's state as the badge and the actions read it. The detail says
 * the state in a string, and the list in the states it knows.
 */
export function badgeOf(
	d: Pick<DockerContainerDetail, 'state' | 'health'>
): Pick<DockerContainerRow, 'state' | 'health' | 'exitCode' | 'oomKilled'> {
	return {
		state: d.state.status as ContainerState,
		health: d.health?.status,
		exitCode: d.state.exitCode,
		oomKilled: d.state.oomKilled
	};
}

/** The seconds from a time to now, or null if there is no time. */
export function secondsSince(t: string | null | undefined, now: number): number | null {
	if (!t) {
		return null;
	}
	const ms = Date.parse(t);
	return Number.isFinite(ms) ? Math.max(0, (now - ms) / 1000) : null;
}

/** A time as "9:56 PM" if it was today, and as "Oct 18" before that. */
export function shortWhen(t: string | null | undefined, now: number): string {
	if (!t) {
		return '—';
	}
	const date = new Date(t);
	if (Number.isNaN(date.getTime())) {
		return '—';
	}
	return date.toDateString() === new Date(now).toDateString()
		? date.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' })
		: date.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

/** A span of milliseconds, such as 250ms or 1.5s, or null if it's missing. */
export function millis(ms: number | null | undefined): string | null {
	if (ms == null || !Number.isFinite(ms)) {
		return null;
	}
	const whole = Math.round(ms);
	return whole >= 1000 ? `${+(whole / 1000).toFixed(1)}s` : `${whole}ms`;
}

/**
 * The command of a health check as one line: a shell command as it is, and an
 * argument list quoted. It is empty for a viewer, who doesn't get the command.
 */
export function healthCommand(test: string[] | null | undefined): string {
	if (!test?.length) {
		return '';
	}
	switch (test[0]) {
		case 'CMD-SHELL':
			return test.slice(1).join(' ');
		case 'CMD':
			return argv(test.slice(1));
	}
	return argv(test);
}

/** How a health check is timed, such as "every 30s", "timeout 3s" and "3 retries". */
export function healthFacts(hc: DockerHealthcheck | null | undefined): string[] {
	const out: string[] = [];
	const interval = millis(hc?.interval || null);
	const timeout = millis(hc?.timeout || null);
	const start = millis(hc?.startPeriod || null);
	if (interval) out.push(`every ${interval}`);
	if (timeout) out.push(`timeout ${timeout}`);
	if (hc?.retries) out.push(plural(hc.retries, 'retry', 'retries'));
	if (start) out.push(`${start} start period`);
	return out;
}

/** Whether a time is one Docker knew. It writes a zero time, in the year 1, for one it doesn't. */
export function knownTime(t: string | null | undefined): boolean {
	return !!t && Date.parse(t) >= Date.UTC(2000, 0, 1);
}

/** How long a health check ran in milliseconds, or null if its times don't say. */
export function checkDuration(r: Pick<DockerHealthResult, 'start' | 'end'>): number | null {
	if (!knownTime(r.start) || !knownTime(r.end)) {
		return null;
	}
	const ms = Date.parse(r.end) - Date.parse(r.start);
	return ms >= 0 && ms < 86_400_000 ? ms : null;
}

const ansi = /\u001b\[[0-9;?]*[ -/]*[@-~]/g;

/** Text without the escape codes that color a terminal's output. */
export function stripAnsi(text: string): string {
	return text.replace(ansi, '');
}

/** What went wrong with a container, for the red banner, or null if nothing did. */
export function crash(
	state: Pick<DockerState, 'running' | 'oomKilled' | 'exitCode' | 'error' | 'finishedAt'>
): { title: string; error?: string; at?: string } | null {
	const failed = state.exitCode != null && !cleanExits.includes(state.exitCode);
	if (!state.oomKilled && !failed && !state.error) {
		return null;
	}
	const title = state.oomKilled
		? 'Killed for running out of memory'
		: failed
			? `Exited with code ${state.exitCode}`
			: "Docker couldn't run it";
	return { title, error: state.error || undefined, at: state.finishedAt };
}

/** A container's ports in the two kinds: published on the machine, and only exposed. */
export function splitPorts(ports: ContainerPort[]): {
	published: ContainerPort[];
	exposed: ContainerPort[];
} {
	return {
		published: ports.filter((p) => p.public != null),
		exposed: ports.filter((p) => p.public == null)
	};
}

/** A port's name as a row's label, such as 80/tcp. */
export function portName(p: ContainerPort): string {
	return `${p.private}/${p.type}`;
}

/** The names of a compose project's files, from the paths Docker keeps in a comma-separated label. */
export function composeFileNames(files: string | null | undefined): string {
	return (files ?? '')
		.split(',')
		.map((f) => f.trim().split('/').pop())
		.filter(Boolean)
		.join(', ');
}

/** A volume's name for showing: an anonymous volume is named by a 64-character ID. */
export function volumeName(name: string | null | undefined): string {
	if (!name) {
		return '';
	}
	return /^[0-9a-f]{64}$/.test(name) ? `${name.slice(0, 12)}…` : name;
}

/** The text of a restart policy, such as "on-failure (max 3)". */
export function restartPolicyLabel(
	policy: DockerContainerDetail['restartPolicy'],
	autoRemove: boolean
): string {
	let text = 'no';
	if (policy && policy.name && policy.name !== 'no') {
		text =
			policy.name === 'on-failure' && policy.maxRetries
				? `${policy.name} (max ${policy.maxRetries})`
				: policy.name;
	}
	return autoRemove ? `${text} · auto-remove` : text;
}

/** What to say where the stats would be, for a container that has no live ones, or null if it has. */
export function statsEnded(
	state: Pick<DockerState, 'running' | 'paused'>,
	streamEnded: string | null
): string | null {
	if (!state.running) {
		return "The container isn't running";
	}
	return state.paused ? 'The container is paused' : streamEnded;
}

// Rows of names and values: the environment and the labels.

/** One name and value in the environment's or the labels' list. */
export interface KeyRow {
	/** Tells the row apart, even if two have one name. */
	id: string;
	key: string;
	value: string;
	/** Whether the value is hidden until someone reveals it. */
	secret: boolean;
}

const secretKey = /pass|secret|token|key|pwd|auth|credential/i;

/** Whether a name looks like it holds a credential. */
export function isSecretKey(key: string): boolean {
	return secretKey.test(key);
}

function sorted(rows: Omit<KeyRow, 'id'>[]): KeyRow[] {
	const seen = new Map<string, number>();
	return rows
		.toSorted((a, b) => a.key.localeCompare(b.key))
		.map((r) => {
			const n = seen.get(r.key) ?? 0;
			seen.set(r.key, n + 1);
			return { ...r, id: n === 0 ? r.key : `${r.key}#${n}` };
		});
}

/** The environment's rows, by name. Every value is hidden until it's revealed, unless it's empty. */
export function envRows(env: DockerEnvVar[] | null | undefined): KeyRow[] {
	return sorted((env ?? []).map((e) => ({ key: e.key, value: e.value, secret: e.value !== '' })));
}

/**
 * The labels' rows, by name. The labels that look like credentials are hidden
 * until they're revealed. Only admins get those, so a viewer's rows are the
 * others.
 */
export function labelRows(
	labels: Record<string, string> | null | undefined,
	secretLabels: Record<string, string> | null | undefined
): KeyRow[] {
	return sorted([
		...Object.entries(labels ?? {}).map(([key, value]) => ({ key, value, secret: false })),
		...Object.entries(secretLabels ?? {}).map(([key, value]) => ({
			key,
			value,
			secret: value !== ''
		}))
	]);
}

/** The rows whose name or value has the filter in it, ignoring case. */
export function filterRows(rows: KeyRow[], query: string): KeyRow[] {
	const q = query.trim().toLowerCase();
	return q
		? rows.filter((r) => r.key.toLowerCase().includes(q) || r.value.toLowerCase().includes(q))
		: rows;
}

// The inspection.

/** One value in a JSON document and where it is, such as `Config.Env[0]`. */
export interface JsonHit {
	path: string;
	/** The value as JSON writes it. */
	text: string;
}

const plainKey = /^[A-Za-z_$][\w$]*$/;

function child(path: string, key: string | number): string {
	if (typeof key === 'number') {
		return `${path}[${key}]`;
	}
	if (plainKey.test(key)) {
		return path ? `${path}.${key}` : key;
	}
	return `${path}[${JSON.stringify(key)}]`;
}

/**
 * The values in a JSON document whose path or text has the query in it,
 * ignoring case, in the order they come: at most `limit` of them, and how many
 * there are in all.
 */
export function searchJson(
	root: unknown,
	query: string,
	limit = 200
): { hits: JsonHit[]; total: number } {
	const q = query.trim().toLowerCase();
	const hits: JsonHit[] = [];
	let total = 0;
	if (!q) {
		return { hits, total };
	}
	const visit = (value: unknown, path: string) => {
		if (value !== null && typeof value === 'object') {
			const entries = Array.isArray(value)
				? value.map((v, i) => [i, v] as const)
				: Object.entries(value);
			if (entries.length > 0) {
				for (const [key, v] of entries) {
					visit(v, child(path, key));
				}
				return;
			}
		}
		const text =
			value !== null && typeof value === 'object' ? JSON.stringify(value) : String(value);
		if (path.toLowerCase().includes(q) || text.toLowerCase().includes(q)) {
			total++;
			if (hits.length < limit) {
				hits.push({ path, text: typeof value === 'string' ? JSON.stringify(value) : text });
			}
		}
	};
	visit(root, '');
	return { hits, total };
}

/** The kind of a piece of JSON text, which decides its color. */
export type JsonKind = 'key' | 'string' | 'literal' | 'number' | 'plain';

export interface JsonToken {
	text: string;
	kind: JsonKind;
}

const jsonToken =
	/("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g;

/** Cuts pretty-printed JSON into pieces that are colored alike. */
export function jsonTokens(text: string): JsonToken[] {
	const out: JsonToken[] = [];
	let last = 0;
	const plain = (to: number) => {
		if (to > last) {
			out.push({ text: text.slice(last, to), kind: 'plain' });
		}
	};
	for (const m of text.matchAll(jsonToken)) {
		plain(m.index);
		if (m[1] !== undefined) {
			out.push({ text: m[1], kind: m[2] ? 'key' : 'string' });
			if (m[2]) {
				out.push({ text: m[2], kind: 'plain' });
			}
		} else if (m[3] !== undefined) {
			out.push({ text: m[3], kind: 'literal' });
		} else {
			out.push({ text: m[4], kind: 'number' });
		}
		last = m.index + m[0].length;
	}
	plain(text.length);
	return out;
}
