// Docker's routes: what is on a device, and what an admin can do to it. The
// device is the ID of a device on the tailnet; Roost works out which Docker
// that is. Logs, processes and the whole inspection are for admins, and so is
// starting and stopping.

import { mutate, request, type HistoryRange } from '#lib/api/client.js';
import type {
	DockerActionResult,
	DockerContainerDetail,
	DockerContainerList,
	DockerHistory,
	DockerImage,
	DockerNetwork,
	DockerOverview,
	DockerProcesses,
	DockerVolume
} from '#lib/api/types.js';

/** What an admin can do to a container. */
export type DockerAction = 'start' | 'stop' | 'restart';

function base(device: string): string {
	return `devices/${encodeURIComponent(device)}/docker`;
}

function container(device: string, id: string, rest = ''): string {
	return `${base(device)}/containers/${encodeURIComponent(id)}${rest}`;
}

/** The Docker daemon on a device, and what it holds. The disk is null until the daemon has measured it. */
export function getDockerOverview(device: string): Promise<DockerOverview> {
	return request('GET', `${base(device)}/overview`);
}

/** Every container on a device, with what each uses now. */
export function getDockerContainers(device: string): Promise<DockerContainerList> {
	return request('GET', `${base(device)}/containers`);
}

/** One container, by name or ID. */
export function getDockerContainer(device: string, id: string): Promise<DockerContainerDetail> {
	return request('GET', container(device, id));
}

/** A container's whole inspection, as Docker writes it. Admins only. */
export function getDockerInspect(device: string, id: string): Promise<unknown> {
	return request('GET', container(device, id, '/inspect'));
}

/** A container's processes, as ps writes them. Admins only. */
export function getDockerProcesses(device: string, id: string): Promise<DockerProcesses> {
	return request('GET', container(device, id, '/top'));
}

/** What a container used over the last range, by the container's name. */
export function getDockerHistory(
	device: string,
	id: string,
	range: HistoryRange
): Promise<DockerHistory> {
	return request('GET', container(device, id, `/history?range=${range}`));
}

export function getDockerVolumes(device: string): Promise<DockerVolume[]> {
	return request('GET', `${base(device)}/volumes`);
}

export function getDockerImages(device: string): Promise<DockerImage[]> {
	return request('GET', `${base(device)}/images`);
}

export function getDockerNetworks(device: string): Promise<DockerNetwork[]> {
	return request('GET', `${base(device)}/networks`);
}

/**
 * Starts, stops or restarts a container. Admins only. If it fails, a toast
 * says why and the error is thrown on. The result says whether anything
 * changed: a container that already runs doesn't start again.
 */
export function runDockerAction(
	device: string,
	id: string,
	action: DockerAction
): Promise<DockerActionResult> {
	return mutate('POST', container(device, id, `/${action}`));
}

/** What to read of a container's logs. */
export interface LogQuery {
	/** Keep the stream open for new lines. It is on for a live view. */
	follow?: boolean;
	/** How many lines from the end to start with, or 'all'. */
	tail?: number | 'all';
	/** Only lines since a time: an RFC 3339 time, or an age such as 15m, 6h or 7d. */
	since?: string;
	stdout?: boolean;
	stderr?: boolean;
	/** For a download: leave Docker's timestamps out. */
	timestamps?: boolean;
}

function logQuery(q: LogQuery): string {
	const params = new URLSearchParams();
	if (q.follow !== undefined) params.set('follow', q.follow ? '1' : '0');
	if (q.tail !== undefined) params.set('tail', String(q.tail));
	if (q.since) params.set('since', q.since);
	if (q.stdout === false) params.set('stdout', '0');
	if (q.stderr === false) params.set('stderr', '0');
	if (q.timestamps === false) params.set('timestamps', '0');
	const s = params.toString();
	return s ? `?${s}` : '';
}

/**
 * The URL of a container's logs as server-sent events: a `start` event, any
 * number of `lines` events, and then `end` or `failure`. Admins only.
 */
export function dockerLogsUrl(device: string, id: string, q: LogQuery = {}): string {
	return `/api/${container(device, id, '/logs')}${logQuery(q)}`;
}

/** The URL of a container's logs as a text file. Admins only. */
export function dockerLogDownloadUrl(device: string, id: string, q: LogQuery = {}): string {
	return `/api/${container(device, id, '/logs/download')}${logQuery(q)}`;
}

/**
 * The URL of a container's live stats as server-sent events: a `backlog`
 * event with the latest samples, then a `sample` event each second, and an
 * `end` event once the container stops.
 */
export function dockerStatsUrl(device: string, id: string): string {
	return `/api/${container(device, id, '/stats')}`;
}
