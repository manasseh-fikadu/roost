// How the app shows checks and the health score they add up to.

import type {
	Check,
	CheckInput,
	CheckKind,
	CheckResult,
	CheckState,
	HealthLabel
} from './api/types.js';
import type { Status } from './status.js';

/** The status each state of a check shows as, for dots and colors. */
export const checkStatus: Record<CheckState, Status> = {
	passing: 'online',
	failing: 'offline',
	pending: 'unknown',
	paused: 'unknown',
	unavailable: 'unknown'
};

export const stateLabels: Record<CheckState, string> = {
	passing: 'Passing',
	failing: 'Failing',
	pending: 'Waiting',
	paused: 'Paused',
	unavailable: 'Unavailable'
};

export const kindLabels: Record<CheckKind, string> = {
	http: 'HTTP',
	tcp: 'TCP',
	dns: 'DNS',
	ping: 'Ping'
};

/** The kinds of check an admin can set up, for a picker. */
export const kindOptions: { value: Exclude<CheckKind, 'ping'>; label: string }[] = [
	{ value: 'http', label: 'HTTP request' },
	{ value: 'tcp', label: 'TCP connection' },
	{ value: 'dns', label: 'DNS lookup' }
];

/**
 * The checks the Checks page lists: all of them, or those passing, failing,
 * or neither, which counts as unavailable, as in the health score.
 */
export type CheckFilter = 'all' | 'passing' | 'failing' | 'unavailable';

const filters: CheckFilter[] = ['all', 'passing', 'failing', 'unavailable'];

/** The filter in a URL's ?state=, or all of them. */
export function parseFilter(s: string | null): CheckFilter {
	return filters.find((f) => f === s) ?? 'all';
}

/** Whether a check is one a filter lists. */
export function matches(c: Check, filter: CheckFilter): boolean {
	switch (filter) {
		case 'all':
			return true;
		case 'passing':
		case 'failing':
			return c.state === filter;
		case 'unavailable':
			return c.state !== 'passing' && c.state !== 'failing';
	}
}

/** A group of checks, as the Checks page lists them. */
export interface CheckGroup {
	name: string;
	checks: Check[];
}

/**
 * Groups checks by their group, keeping the order they come in. The hub
 * sends them by group, with the ping checks last.
 */
export function groupChecks(checks: Check[]): CheckGroup[] {
	const groups = new Map<string, Check[]>();
	for (const c of checks) {
		const list = groups.get(c.group);
		if (list) {
			list.push(c);
		} else {
			groups.set(c.group, [c]);
		}
	}
	return [...groups].map(([name, checks]) => ({ name, checks }));
}

/** How many of each check's latest results the app keeps, as the hub does. */
export const recentCount = 30;

/** Each check's latest results, oldest first, by check ID. */
export type ResultsByCheck = Record<string, CheckResult[]>;

/**
 * Adds each check's last result to its latest results, if it's newer than
 * the ones there, keeping the latest `recentCount`. It returns `byId` itself
 * if nothing is new.
 */
export function addLatest(byId: ResultsByCheck, checks: Check[]): ResultsByCheck {
	let out = byId;
	for (const c of checks) {
		const last = c.last;
		const list = byId[c.id] ?? [];
		const prev = list.at(-1);
		if (!last || (prev && Date.parse(prev.at) >= Date.parse(last.at))) {
			continue;
		}
		if (out === byId) {
			out = { ...byId };
		}
		out[c.id] = [...list.slice(-(recentCount - 1)), last];
	}
	return out;
}

/** What a check probes: its whole target for admins, and the part that holds no secret for viewers. */
export function targetOf(c: Check): string {
	return c.target || c.endpoint;
}

/**
 * What PUT /api/checks/{id} takes to keep a check as it is, with changes.
 * Only admins can change checks, and they get the whole target.
 */
export function inputOf(c: Check, changes: Partial<CheckInput> = {}): CheckInput {
	return {
		name: c.name,
		kind: c.kind,
		target: c.target,
		group: c.group,
		intervalSec: c.intervalSec,
		timeoutMs: c.timeoutMs,
		expectStatus: c.expectStatus,
		insecure: c.insecure,
		paused: c.paused,
		...changes
	};
}

/** How often a check runs, such as "every 30s" or "every 5m". */
export function every(seconds: number): string {
	if (seconds % 3600 === 0) {
		return `every ${seconds / 3600}h`;
	}
	return seconds % 60 === 0 ? `every ${seconds / 60}m` : `every ${seconds}s`;
}

export const healthLabels: Record<HealthLabel, string> = {
	healthy: 'Healthy',
	degraded: 'Degraded',
	critical: 'Critical',
	unknown: 'No score yet'
};

/** How good a health score is: healthy at 90 and up, degraded at 70, critical below, or none. */
function scoreTone(score: number | null): 'good' | 'fair' | 'bad' | 'none' {
	if (score === null) {
		return 'none';
	}
	return score >= 90 ? 'good' : score >= 70 ? 'fair' : 'bad';
}

const scoreColors = {
	good: 'var(--color-green-fg)',
	fair: 'var(--color-amber)',
	bad: 'var(--color-red-fg)',
	none: 'var(--color-line-heavy)'
};

const scoreClasses = {
	good: 'text-green-fg',
	fair: 'text-amber-fg',
	bad: 'text-red-fg',
	none: 'text-fg-muted'
};

/** The CSS color of a health score, for rings and lines. */
export function scoreColor(score: number | null): string {
	return scoreColors[scoreTone(score)];
}

/** The text class of a health score's color. */
export function scoreClass(score: number | null): string {
	return scoreClasses[scoreTone(score)];
}
