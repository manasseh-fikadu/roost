// What a device's agent is doing, for the Agents page.

import type { Device } from './api/types.js';
import { ago } from './format.js';
import type { Status } from './status.js';

/** Whether the agent runs on an OS, as Tailscale names it. */
export function agentRuns(os: string): boolean {
	return ['linux', 'macos'].includes(os.toLowerCase());
}

export interface AgentState {
	label: string;
	status: Status;
	/** A line about it, after the device's OS. */
	detail?: string;
}

/** Where a device's agent is at, by the hub's clock `now`. */
export function agentState(d: Device, now: number): AgentState {
	const a = d.agent;
	if (d.measuredBy === 'hub') {
		return { label: 'Built in', status: 'online', detail: 'Roost runs here and measures it' };
	}
	if (!a) {
		return agentRuns(d.os)
			? { label: 'Not installed', status: 'unknown' }
			: { label: 'Not supported', status: 'unknown' };
	}
	if (!a.lastSeen) {
		return {
			label: 'Waiting for a heartbeat',
			status: 'unknown',
			detail: `enrolled ${ago(a.enrolledAt, now)}`
		};
	}
	const last = `last heartbeat ${ago(a.lastSeen, now)}`;
	if (!a.enrolledAt) {
		// Its heartbeats stopped when enrollment replaced the shared token.
		return { label: 'Not enrolled', status: 'attention', detail: `v${a.version} · ${last}` };
	}
	if (!d.online) {
		return { label: 'Offline', status: 'unknown', detail: last };
	}
	if (a.stale) {
		return { label: 'Stopped reporting', status: 'attention', detail: last };
	}
	const detail = `v${a.version} · heartbeat ${ago(a.lastSeen, now)}`;
	if (a.outdated) {
		return { label: 'Update available', status: 'attention', detail };
	}
	return { label: 'Reporting', status: 'online', detail };
}
