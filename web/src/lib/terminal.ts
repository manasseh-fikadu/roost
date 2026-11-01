// What the Terminal page keeps track of: its tabs, which devices it can
// open terminals on, and how much output to ack. The terminal itself is
// TerminalView.svelte.

import type { Device, DeviceSSH, SSHRefusal, SSHState } from './api/types.js';

/** A tab on the Terminal page. */
export interface TerminalTab {
	/** Tells the tab apart from the others. */
	key: string;
	deviceId: string;
	/** The tmux session it's attached to, or null for a shell that ends with the tab. */
	session: string | null;
	/** Where the shell starts, or null for the user's home. */
	cwd: string | null;
}

/** Where a terminal's connection is. */
export interface TermStatus {
	state: 'connecting' | 'open' | 'closed' | 'error';
	/** Why it closed, or what went wrong. */
	message?: string;
}

/**
 * What to tell someone whose terminal ended. The hub sends no status when
 * the connection to the machine dropped, so the shell, and the tmux session
 * it was attached to, may still be running. A tmux client that exits with 0
 * may only have detached, and Bield can't tell that from its last shell
 * exiting, so it doesn't say the session ended.
 */
export function exitMessage(session: string | null, code: number | null | undefined): string {
	if (code == null) {
		return session
			? `Lost the connection to ${session}. It may still be running on the machine.`
			: 'Lost the connection to the shell.';
	}
	if (session) {
		if (code === 0) {
			return `Detached from ${session}, or the session ended.`;
		}
		// What sh says when the command isn't there.
		return code === 127
			? "tmux isn't installed on the machine."
			: `tmux exited with status ${code}.`;
	}
	return code === 0 ? 'The shell exited.' : `The shell exited with status ${code}.`;
}

/** Where the Terminal page keeps its tabs in localStorage. */
export const tabsKey = 'bield.terminal.tabs';

/** The devices Bield has SSH access to, with the machine Bield runs on once. */
export function terminalDevices(devices: Device[]): Device[] {
	const set = devices.filter((d) => d.ssh && d.ssh.state !== 'not-set-up');
	// The machine Bield runs on can be two nodes, which share access. Its
	// own node is the one with its name.
	const hubs = set.filter((d) => d.ssh?.hub);
	const drop = hubs.length > 1 ? hubs.find((d) => d.self) : undefined;
	return set.filter((d) => d !== drop);
}

/**
 * The ID a device is listed under on the Terminal page. The machine Bield
 * runs on can be two nodes that share one SSH access, and the page lists only
 * one (see terminalDevices), so a link to the other, such as the Terminal
 * button on Bield's own node in embedded mode, means the one that's listed.
 * Any other ID stays as it is.
 */
export function listedDeviceId(devices: Device[], id: string): string {
	const listed = terminalDevices(devices);
	if (listed.some((d) => d.id === id)) {
		return id;
	}
	const dropped = devices.find((d) => d.id === id);
	return (dropped?.ssh?.hub && listed.find((d) => d.ssh?.hub)?.id) || id;
}

/** Whether a terminal can open on a device now. */
export function canOpen(d: Device): boolean {
	const s = d.ssh;
	return !!s && s.state !== 'not-set-up' && s.state !== 'host-key-changed' && (d.online || !!s.hub);
}

/**
 * The tabs saved in localStorage, for devices that still have SSH access.
 * Only tmux tabs come back: a plain shell ended when its tab did.
 */
export function loadTabs(saved: string | null, devices: Device[]): TerminalTab[] {
	let raw: unknown;
	try {
		raw = JSON.parse(saved ?? '[]');
	} catch {
		return [];
	}
	if (!Array.isArray(raw)) {
		return [];
	}
	const ids = new Set(terminalDevices(devices).map((d) => d.id));
	const tabs: TerminalTab[] = [];
	for (const t of raw) {
		if (
			typeof t?.key !== 'string' ||
			typeof t.deviceId !== 'string' ||
			typeof t.session !== 'string' ||
			tabs.some((o) => o.key === t.key)
		) {
			continue;
		}
		// A tab on the machine Bield runs on follows the node that's listed.
		const deviceId = listedDeviceId(devices, t.deviceId);
		if (ids.has(deviceId)) {
			tabs.push({ key: t.key, deviceId, session: t.session, cwd: null });
		}
	}
	return tabs;
}

/** The tabs to save in localStorage: the tmux ones. */
export function saveTabs(tabs: TerminalTab[]): string {
	return JSON.stringify(
		tabs.filter((t) => t.session).map(({ key, deviceId, session }) => ({ key, deviceId, session }))
	);
}

/** The first name like web-1 that isn't taken. */
export function nextSessionName(taken: Iterable<string>): string {
	const used = new Set(taken);
	for (let i = 1; ; i++) {
		if (!used.has(`web-${i}`)) {
			return `web-${i}`;
		}
	}
}

/** The URL of a terminal's WebSocket, on the page's own origin. */
export function terminalURL(
	location: { protocol: string; host: string },
	deviceId: string,
	opts: { session: string | null; cwd: string | null; cols: number; rows: number }
): string {
	const q = new URLSearchParams({ cols: String(opts.cols), rows: String(opts.rows) });
	if (opts.session) {
		q.set('session', opts.session);
	}
	if (opts.cwd) {
		q.set('cwd', opts.cwd);
	}
	const scheme = location.protocol === 'https:' ? 'wss' : 'ws';
	return `${scheme}://${location.host}/api/devices/${encodeURIComponent(deviceId)}/terminal?${q}`;
}

/**
 * Counts the output written to the screen, and says when to ack it. The
 * hub sends a window of output ahead of the acks, so an ack every so often
 * keeps it coming without a message for every chunk.
 */
export class Acker {
	#written = 0;
	readonly every: number;

	constructor(every = 64 * 1024) {
		this.every = every;
	}

	/** Counts n more bytes written, and returns how many to ack now, or 0. */
	wrote(n: number): number {
		this.#written += n;
		if (this.#written < this.every) {
			return 0;
		}
		const ack = this.#written;
		this.#written = 0;
		return ack;
	}

	/** Returns the bytes written and not acked yet, and forgets them. */
	flush(): number {
		const ack = this.#written;
		this.#written = 0;
		return ack;
	}
}

/** Keys that phone keyboards lack, for the row under the terminal. */
export const extraKeys: { label: string; data: string }[] = [
	{ label: 'esc', data: '\x1b' },
	{ label: 'tab', data: '\t' },
	{ label: '^C', data: '\x03' },
	{ label: '^D', data: '\x04' },
	{ label: '^L', data: '\x0c' },
	{ label: '↑', data: '\x1b[A' },
	{ label: '↓', data: '\x1b[B' },
	{ label: '←', data: '\x1b[D' },
	{ label: '→', data: '\x1b[C' },
	{ label: '|', data: '|' },
	{ label: '~', data: '~' },
	{ label: '/', data: '/' }
];

/** The port sshd listens on unless an admin says otherwise. */
export const defaultSSHPort = 22;

/**
 * Reads what's typed in a port field: nothing is the default port, and
 * anything but a whole number from 1 to 65535 is null, so a mistake such as
 * `22a` isn't turned into a port.
 */
export function parsePort(text: string): number | null {
	const t = text.trim();
	if (t === '') {
		return defaultSSHPort;
	}
	if (!/^[0-9]{1,5}$/.test(t)) {
		return null;
	}
	const n = Number(t);
	return n >= 1 && n <= 65535 ? n : null;
}

const stateLabels: Record<SSHState, string> = {
	'not-set-up': 'Not set up',
	unchecked: 'Not checked yet',
	ok: 'Connected',
	failing: "Can't log in",
	'host-key-changed': 'Host key changed'
};

/** What SSH access's state means, in a few words. */
export function sshStateLabel(s: DeviceSSH): string {
	if (s.state === 'ok' && s.method === 'tailscale') {
		return 'Connected with Tailscale SSH';
	}
	return stateLabels[s.state];
}

/**
 * The user most devices' SSH access logs in as, to suggest for the next one,
 * or '' if none is set up. The first of a tie wins.
 */
export function commonSSHUser(devices: readonly Device[]): string {
	const counts = new Map<string, number>();
	for (const d of devices) {
		const user = d.ssh?.user;
		if (user && !d.ssh?.hub) {
			counts.set(user, (counts.get(user) ?? 0) + 1);
		}
	}
	let best = '';
	let most = 0;
	for (const [user, n] of counts) {
		if (n > most) {
			best = user;
			most = n;
		}
	}
	return best;
}

/** What a Tailscale SSH refusal means, and so what to change. */
export function refusalText(
	refusal: SSHRefusal,
	deviceName: string,
	user: string
): { title: string; detail: string } {
	switch (refusal) {
		case 'check':
			return {
				title: 'Tailscale SSH wants a browser check',
				detail:
					'The rule that covers Bield has the action "check", which asks for a visit to a link ' +
					'each time, and Bield can\'t make one. Bield needs a rule with the action "accept".'
			};
		case 'node':
			return {
				title: "The tailnet policy doesn't let Bield SSH here",
				detail: `No rule in the "ssh" section of your policy covers Bield logging in to ${deviceName}.`
			};
		case 'user':
			return {
				title: `The tailnet policy doesn't let Bield log in as ${user}`,
				detail: `A rule lets Bield SSH to ${deviceName}, but its "users" don't include ${user}, or ${deviceName} has no account named ${user}.`
			};
	}
}
