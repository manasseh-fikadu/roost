// What the first-run setup shows: whether Bield can see what it needs to,
// with a hint for each thing it can't, and when to send an admin there.

import { roleLabel } from './access.js';
import type { Collector, Device, Me, Snapshot, TailscaleMode } from './api/types.js';
import { plural } from './format.js';
import type { Status } from './status.js';

/** One thing the setup checks, and what to do about it if it's not right. */
export interface SetupCheck {
	id: 'tailscale' | 'signin' | 'docker' | 'systemd';
	label: string;
	status: Status;
	/** What Bield found, in a line. */
	detail: string;
	/** What to do about it, if there's something to. */
	hint?: string;
}

const tailscaleCollector = 'Tailscale status';
const dockerCollector = 'Docker';
const systemdCollector = 'systemd units';

function collector(s: Snapshot, name: string): Collector | undefined {
	return s.collectors.find((c) => c.name === name);
}

/** Bield's checks of Tailscale, who's signed in, Docker and systemd, in order. */
export function setupChecks(
	snapshot: Snapshot | null,
	me: Me | null | undefined,
	mode: TailscaleMode | undefined
): SetupCheck[] {
	return [
		tailscaleCheck(snapshot, mode),
		signInCheck(me),
		dockerCheck(snapshot),
		systemdCheck(snapshot)
	];
}

const checking = 'Checking…';

function tailscaleCheck(s: Snapshot | null, mode: TailscaleMode | undefined): SetupCheck {
	const base = { id: 'tailscale', label: 'Tailscale' } as const;
	const c = s && collector(s, tailscaleCollector);
	if (!s || !c || c.state === 'waiting') {
		return { ...base, status: 'unknown', detail: checking };
	}
	if (c.state === 'failing') {
		return {
			...base,
			status: 'attention',
			detail: `Bield can't read the tailnet${c.error ? `: ${c.error}` : '.'}`,
			hint:
				mode === 'host'
					? 'Make sure Tailscale is running on this machine (tailscale status), and that the user Bield runs as may use it: sudo tailscale set --operator=$USER. Then restart Bield.'
					: 'Restart Bield. If its node was logged out of the tailnet, open the setup page on its machine to log it in again.'
		};
	}
	const others = s.devices.filter((d) => !d.self).length;
	const how =
		mode === 'embedded'
			? 'Bield is its own machine on the tailnet'
			: mode === 'host'
				? "Using this machine's Tailscale"
				: 'Connected to the tailnet';
	const parts = [how];
	// Set when the tailnet isn't Tailscale's, such as when it's Headscale.
	const control = s.hub.controlUrl;
	if (control) {
		parts.push(`control server ${controlHost(control)}`);
	}
	if (s.hub.tailnet) {
		parts.push(`tailnet ${s.hub.tailnet}`);
	}
	parts.push(`${plural(others, 'other device')} found`);
	if (others === 0) {
		const login = control ? `, as in tailscale up --login-server ${control}` : '';
		return {
			...base,
			status: 'attention',
			detail: parts.join(' · '),
			hint: `Bield sees no other machine yet. Install Tailscale on one and log it in to this tailnet${login}, and it shows up here within seconds.`
		};
	}
	return { ...base, status: 'online', detail: parts.join(' · ') };
}

/** A control server's address without the scheme, as in headscale.example.com:8443. */
function controlHost(url: string): string {
	try {
		return new URL(url).host;
	} catch {
		return url;
	}
}

function signInCheck(me: Me | null | undefined): SetupCheck {
	const base = { id: 'signin', label: 'Signed in' } as const;
	if (!me?.signedIn) {
		return {
			...base,
			status: 'attention',
			detail: "Bield doesn't know who you are.",
			hint: 'Open Bield from a device on the tailnet that you are logged in to.'
		};
	}
	const who = me.name && me.login ? `${me.name} (${me.login})` : (me.name ?? me.login ?? 'someone');
	const on = me.device ? ` on ${me.device.split('.')[0]}` : '';
	const role = me.role ? ` · ${roleLabel(me.role)}` : '';
	return { ...base, status: 'online', detail: `${who}${on}${role}` };
}

function dockerCheck(s: Snapshot | null): SetupCheck {
	const base = { id: 'docker', label: 'Docker' } as const;
	if (!s) {
		return { ...base, status: 'unknown', detail: checking };
	}
	const c = collector(s, dockerCollector);
	if (!c) {
		return {
			...base,
			status: 'unknown',
			detail: 'No Docker socket on the machine Bield runs on.',
			hint: 'Skip this if you don’t use Docker there. Docker on other machines works once Bield has SSH access to them.'
		};
	}
	switch (c.state) {
		case 'waiting':
			return { ...base, status: 'unknown', detail: checking };
		case 'failing':
			return {
				...base,
				status: 'attention',
				detail: dockerDetail(c.error ?? ''),
				hint: dockerHint(c.error ?? '')
			};
		case 'ok':
			return {
				...base,
				status: 'online',
				detail: `Bield can read Docker · ${plural(s.containers.length, 'container')}`
			};
	}
}

/** What went wrong with Docker's socket, in a line: the error has more than that. */
export function dockerDetail(error: string): string {
	if (error.includes('docker group')) {
		return "Bield can't open the Docker socket: permission denied.";
	}
	if (error.includes('no socket at')) {
		return 'There is no Docker socket on this machine.';
	}
	if (error.includes('nothing listens on')) {
		return 'Docker has a socket, but nothing answers on it.';
	}
	return error || "Bield can't read Docker.";
}

/** What to do about the error that Bield got from Docker's socket. */
export function dockerHint(error: string): string {
	if (error.includes('docker group')) {
		return 'Add the user Bield runs as to the docker group (sudo usermod -aG docker $USER), then restart Bield.';
	}
	if (error.includes('no socket at')) {
		return 'Docker isn’t installed or isn’t running there. Start it, or skip this if you don’t use Docker.';
	}
	if (error.includes('nothing listens on')) {
		return 'Docker isn’t running. Start it (sudo systemctl start docker), and Bield picks it up.';
	}
	return 'Make sure Docker runs and that the user Bield runs as may use its socket.';
}

function systemdCheck(s: Snapshot | null): SetupCheck {
	const base = { id: 'systemd', label: 'systemd' } as const;
	if (!s) {
		return { ...base, status: 'unknown', detail: checking };
	}
	const c = collector(s, systemdCollector);
	if (!c) {
		return {
			...base,
			status: 'unknown',
			detail: "The machine Bield runs on doesn't run systemd.",
			hint: 'Nothing to do: Bield shows the health of services on machines that run systemd.'
		};
	}
	switch (c.state) {
		case 'waiting':
			return { ...base, status: 'unknown', detail: checking };
		case 'failing':
			return {
				...base,
				status: 'attention',
				detail: c.error ?? "Bield can't read systemd.",
				hint: 'Bield runs systemctl as its own user. Make sure that user can run systemctl list-units without a password prompt.'
			};
		case 'ok':
			return {
				...base,
				status: 'online',
				detail: `Bield can read systemd · ${plural(s.units.length, 'unit')} watched`
			};
	}
}

/** The steps of the setup, in order. */
export const setupSteps = [
	{ value: 'checks', label: 'Check' },
	{ value: 'name', label: 'Name and admins' },
	{ value: 'connect', label: 'Connect more' }
] as const;

export type SetupStep = (typeof setupSteps)[number]['value'];

/** The step a ?step= asks for, or the first one. */
export function stepOf(param: string | null): SetupStep {
	return setupSteps.find((s) => s.value === param)?.value ?? 'checks';
}

/** The step after one, or null after the last. */
export function nextStep(step: SetupStep): SetupStep | null {
	const i = setupSteps.findIndex((s) => s.value === step);
	return setupSteps[i + 1]?.value ?? null;
}

/**
 * Whether to send the caller to the setup: they're an admin, the hub says no
 * one has finished it, and they haven't been sent in this browser tab.
 */
export function shouldOpenSetup(
	admin: boolean,
	hub: { setupDone: boolean } | undefined,
	alreadySent: boolean
): boolean {
	return admin && hub !== undefined && !hub.setupDone && !alreadySent;
}

const sentKey = 'bield:setup-sent';

/** Whether this browser tab has been sent to the setup, or has left it. */
export function setupSent(): boolean {
	try {
		return sessionStorage.getItem(sentKey) === '1';
	} catch {
		// Storage can be off, such as in a private window. The setup then
		// opens again on the next visit to the overview, which is harmless.
		return false;
	}
}

/**
 * Remembers that this tab has been sent to the setup, so the overview doesn't
 * send it again, such as when an admin leaves without finishing, or finishes
 * before the hub's snapshot says so.
 */
export function markSetupSent(): void {
	try {
		sessionStorage.setItem(sentKey, '1');
	} catch {
		// See setupSent.
	}
}

/**
 * The device to set up SSH access to next: the first that's online and that
 * Bield can log in to but doesn't yet. The machine Bield runs on comes first,
 * where it takes one click.
 */
export function sshCandidate(devices: readonly Device[]): Device | undefined {
	return devices.find((d) => d.online && d.ssh?.state === 'not-set-up');
}
