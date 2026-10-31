// Containers as the Docker pages show them. The server decides what a viewer
// may see; these only decide how it reads.

import type { ContainerPort, DockerContainerRow, DockerImage } from '#lib/api/types.js';
import type { DockerAction } from './api.js';
import type { Status, Tone } from '#lib/status.js';

/** Exit codes of a container that stopped as it was told to: 0, and the ones a stop leaves. */
export const cleanExits = [0, 130, 137, 143];

/** A short container ID, as `docker ps` shows it. */
export function shortId(id: string): string {
	return id.replace(/^sha256:/, '').slice(0, 12);
}

/** Whether a container is running, or about to be. */
export function isUp(state: string): boolean {
	return state === 'running' || state === 'restarting' || state === 'paused';
}

/**
 * A container's state as a status: running and healthy is online, one that
 * is unhealthy, restarting or paused needs attention, one that crashed is
 * offline, and one that stopped cleanly is unknown, since stopping is a thing
 * to do.
 */
export function rowStatus(
	c: Pick<DockerContainerRow, 'state' | 'health' | 'exitCode' | 'oomKilled'>
): Status {
	switch (c.state) {
		case 'running':
			return c.health === 'unhealthy' ? 'attention' : 'online';
		case 'restarting':
		case 'paused':
			return 'attention';
		case 'dead':
			return 'offline';
		case 'exited':
			return c.oomKilled || (c.exitCode != null && !cleanExits.includes(c.exitCode))
				? 'offline'
				: 'unknown';
	}
	return 'unknown';
}

/** How to label a container's state: a word, and the color of its badge. */
export function stateBadge(
	c: Pick<DockerContainerRow, 'state' | 'health' | 'exitCode' | 'oomKilled'>
): { label: string; tone: Tone } {
	switch (c.state) {
		case 'running':
			if (c.health === 'unhealthy') return { label: 'unhealthy', tone: 'amber' };
			if (c.health === 'starting') return { label: 'starting', tone: 'blue' };
			return { label: 'running', tone: 'green' };
		case 'restarting':
			return { label: 'restarting', tone: 'amber' };
		case 'paused':
			return { label: 'paused', tone: 'blue' };
		case 'dead':
			return { label: 'dead', tone: 'red' };
		case 'exited':
			if (c.oomKilled) return { label: 'out of memory', tone: 'red' };
			if (c.exitCode != null && !cleanExits.includes(c.exitCode)) {
				return { label: `exited (${c.exitCode})`, tone: 'red' };
			}
			return { label: 'stopped', tone: 'gray' };
	}
	return { label: c.state, tone: 'gray' };
}

/** The actions that make sense for a container in a state. */
export function actionsFor(state: string): DockerAction[] {
	switch (state) {
		case 'running':
		case 'restarting':
			return ['restart', 'stop'];
		case 'paused':
			return ['stop'];
		default:
			return ['start'];
	}
}

/** The container's published ports as `8080→80/udp`, or `80` if it's not published. */
export function portLabel(p: ContainerPort): string {
	const base = p.public ? `${p.public}→${p.private}` : `${p.private}`;
	return p.type === 'tcp' ? base : `${base}/${p.type}`;
}

/** An argument list as one line, quoting what needs it. */
export function argv(args: string[] | null | undefined): string {
	return (args ?? []).map((a) => (/^[\w@%+=:,./-]+$/.test(a) ? a : JSON.stringify(a))).join(' ');
}

/** The containers of one compose project, or of none. */
export interface RowGroup {
	/** The project, or null for the containers in none. */
	project: string | null;
	containers: DockerContainerRow[];
}

/** Groups containers by compose project, by name, and then the ones in none. */
export function groupRows(rows: DockerContainerRow[]): RowGroup[] {
	const projects = new Map<string, DockerContainerRow[]>();
	const loose: DockerContainerRow[] = [];
	for (const c of rows) {
		if (!c.project) {
			loose.push(c);
		} else {
			projects.set(c.project, [...(projects.get(c.project) ?? []), c]);
		}
	}
	const out: RowGroup[] = [...projects]
		.sort(([a], [b]) => a.localeCompare(b))
		.map(([project, containers]) => ({ project, containers }));
	if (loose.length > 0) {
		out.push({ project: null, containers: loose });
	}
	return out;
}

/** Whether a container matches a filter: every word of it is somewhere in its details. */
export function matchesFilter(c: DockerContainerRow, query: string): boolean {
	const words = query.toLowerCase().split(/\s+/).filter(Boolean);
	if (words.length === 0) {
		return true;
	}
	const hay = [
		c.name,
		c.image,
		c.project,
		c.service,
		c.state,
		c.health,
		c.status,
		...c.ports.map(portLabel),
		...c.networks
	]
		.join(' ')
		.toLowerCase();
	return words.every((w) => hay.includes(w));
}

/** An image's name for showing: its first tag, or its short ID if it has none. */
export function imageName(i: DockerImage): string {
	return i.tags[0] ?? shortId(i.id);
}

/** Totals for the summary tiles, across the running containers. */
export function totals(rows: DockerContainerRow[]) {
	const running = rows.filter((c) => c.state === 'running');
	const sum = (pick: (s: NonNullable<DockerContainerRow['stats']>) => number | null) =>
		running.reduce((n, c) => n + ((c.stats && pick(c.stats)) ?? 0), 0);
	return {
		total: rows.length,
		running: running.length,
		notRunning: rows.length - running.length,
		// Docker keeps the last health of a container that stopped.
		unhealthy: running.filter((c) => c.health === 'unhealthy').length,
		cpu: sum((s) => s.cpu),
		mem: sum((s) => s.memUsed),
		rx: sum((s) => s.netRx),
		tx: sum((s) => s.netTx),
		cpus: running.find((c) => c.stats)?.stats?.cpus ?? null
	};
}
