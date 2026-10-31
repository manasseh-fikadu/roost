// How the app shows alerts, the activity log, and the services on the hub:
// its containers and systemd units.

import type {
	Alert,
	AlertSeverity,
	Container,
	Device,
	EventKind,
	EventLevel,
	Unit
} from './api/types.js';
import type { Status, Tone } from './status.js';

interface SeverityMeta {
	label: string;
	tone: Tone;
	/** The text class of its color. */
	text: string;
	/** The classes of a card's border and background. */
	box: string;
}

export const severityMeta: Record<AlertSeverity, SeverityMeta> = {
	critical: {
		label: 'Critical',
		tone: 'red',
		text: 'text-red-fg',
		box: 'border-red/30 bg-red/[0.06]'
	},
	warning: {
		label: 'Warning',
		tone: 'amber',
		text: 'text-amber-fg',
		box: 'border-amber/25 bg-amber/[0.05]'
	},
	info: {
		label: 'Info',
		tone: 'blue',
		text: 'text-blue-fg',
		box: 'border-blue-fg/20 bg-blue/[0.05]'
	}
};

/** How long an admin can snooze an alert for, in a menu. */
export const snoozeOptions: { minutes: number; label: string }[] = [
	{ minutes: 60, label: '1 hour' },
	{ minutes: 240, label: '4 hours' },
	{ minutes: 1440, label: '1 day' },
	{ minutes: 10080, label: '1 week' }
];

/** The alerts that need attention now: open or acknowledged, and not snoozed. */
export function activeAlerts(alerts: Alert[]): Alert[] {
	return alerts.filter((a) => a.state === 'open' || a.state === 'acknowledged');
}

/** The tabs of the Alerts page. */
export type AlertTab = 'open' | 'snoozed' | 'resolved';

/** The tab in a URL's ?tab=, or the open alerts. */
export function parseAlertTab(s: string | null): AlertTab {
	return s === 'snoozed' || s === 'resolved' ? s : 'open';
}

/**
 * Whether a device is the machine Roost runs on. In embedded mode, that
 * machine is two nodes: Roost's own, and the machine's.
 */
export function onHub(d: Device): boolean {
	return d.measuredBy === 'hub';
}

/**
 * The IDs a device's alerts, units and containers can be under: its own,
 * and on the hub's machine, its other node's.
 */
export function deviceIds(device: Device, devices: Device[]): Set<string> {
	if (!onHub(device)) {
		return new Set([device.id]);
	}
	return new Set(devices.filter(onHub).map((d) => d.id));
}

/** The items about a device, such as its alerts or units. */
export function onDevice<T extends { deviceId?: string }>(
	items: T[],
	device: Device,
	devices: Device[]
): T[] {
	const ids = deviceIds(device, devices);
	return items.filter((i) => i.deviceId !== undefined && ids.has(i.deviceId));
}

/** A systemd unit's state as a status. */
export function unitStatus(u: Unit): Status {
	switch (u.active) {
		case 'active':
			return 'online';
		case 'failed':
			return 'offline';
		case 'activating':
		case 'reloading':
		case 'deactivating':
			return 'attention';
	}
	return u.watched ? 'attention' : 'unknown';
}

/** A container's state as a status. */
export function containerStatus(c: Container): Status {
	if (c.problem) {
		return c.state === 'running' || c.state === 'restarting' || c.problem === 'stopped'
			? 'attention'
			: 'offline';
	}
	return c.state === 'running' ? 'online' : 'unknown';
}

/** The containers of one compose project, or of none. */
export interface ContainerGroup {
	/** The project, or null for the containers in none. */
	project: string | null;
	containers: Container[];
}

/** Groups containers by compose project, by name, and then those in none. */
export function groupContainers(containers: Container[]): ContainerGroup[] {
	const projects = new Map<string, Container[]>();
	const loose: Container[] = [];
	for (const c of containers) {
		if (!c.project) {
			loose.push(c);
			continue;
		}
		const list = projects.get(c.project);
		if (list) {
			list.push(c);
		} else {
			projects.set(c.project, [c]);
		}
	}
	const out: ContainerGroup[] = [...projects]
		.sort(([a], [b]) => a.localeCompare(b))
		.map(([project, containers]) => ({ project, containers }));
	if (loose.length > 0) {
		out.push({ project: null, containers: loose });
	}
	return out;
}

export const eventKindLabels: Record<EventKind, string> = {
	device: 'Devices',
	agent: 'Agents',
	alert: 'Alerts',
	check: 'Checks',
	container: 'Containers',
	access: 'Access',
	settings: 'Settings',
	ssh: 'SSH',
	terminal: 'Terminals',
	files: 'Files',
	notification: 'Notifications'
};

/** The kinds of event only admins see: who has a role, SSH access, terminals and files. */
export const adminEventKinds: EventKind[] = ['access', 'ssh', 'terminal', 'files'];

/** The text class of an event's level. */
export const eventLevelText: Record<EventLevel, string> = {
	info: 'text-fg-subtle',
	success: 'text-green-fg',
	warning: 'text-amber-fg',
	error: 'text-red-fg'
};

/** An event's level as a status, for its dot. */
export const eventLevelStatus: Record<EventLevel, Status> = {
	info: 'unknown',
	success: 'online',
	warning: 'attention',
	error: 'offline'
};

/**
 * Merges a fresh page of items, such as events or resolved alerts, into the
 * ones loaded: once each, the fresh one of an ID winning, newest (highest
 * ID) first.
 */
export function mergeById<T extends { id: number }>(loaded: T[], fresh: T[]): T[] {
	const ids = new Set(fresh.map((x) => x.id));
	return [...fresh, ...loaded.filter((x) => !ids.has(x.id))].sort((a, b) => b.id - a.id);
}

/** A service on the network map: a compose project, a container in none, or a systemd unit. */
export interface MapService {
	id: string;
	kind: 'compose' | 'container' | 'unit';
	name: string;
	status: Status;
	subtitle: string;
	/** Host ports its containers publish. */
	ports: number[];
	/** The memory a unit uses, in bytes. */
	memory?: number;
}

/**
 * Sums up statuses: online if all are, offline if all are, unknown if none
 * says, and otherwise attention.
 */
function overall(statuses: Status[]): Status {
	const known = statuses.filter((s) => s !== 'unknown');
	if (known.length === 0) {
		return 'unknown';
	}
	if (known.every((s) => s === known[0]) && known[0] !== 'attention') {
		return known[0];
	}
	return 'attention';
}

/**
 * The services on the hub, for the network map: each compose project, each
 * container in none, and each systemd unit Roost watches or that failed.
 * One whose check fails needs attention.
 */
export function mapServices(
	containers: Container[],
	units: Unit[],
	failingChecks: Set<string>
): MapService[] {
	const out: MapService[] = [];
	for (const g of groupContainers(containers)) {
		const members = g.project ? [g.containers] : g.containers.map((c) => [c]);
		for (const cs of members) {
			let status = overall(cs.map(containerStatus));
			if (status === 'online' && cs.some((c) => c.checkIds.some((id) => failingChecks.has(id)))) {
				status = 'attention';
			}
			const running = cs.filter((c) => c.state === 'running').length;
			const one = cs[0];
			out.push({
				id: g.project ? `compose:${g.project}` : `container:${one.name}`,
				kind: g.project ? 'compose' : 'container',
				name: g.project ?? one.name,
				status,
				subtitle: g.project ? `${running}/${cs.length} running` : one.problem || one.status,
				ports: [
					...new Set(cs.flatMap((c) => c.ports.flatMap((p) => (p.public ? [p.public] : []))))
				].sort((a, b) => a - b)
			});
		}
	}
	for (const u of units) {
		out.push({
			id: `unit:${u.scope}:${u.name}`,
			kind: 'unit',
			name: u.name,
			status: unitStatus(u),
			subtitle: `${u.active} · ${u.sub}`,
			ports: [],
			memory: u.memory
		});
	}
	return out;
}
