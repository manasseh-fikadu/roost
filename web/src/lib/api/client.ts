import { toast } from '#lib/toast.svelte.js';
import type {
	AgentCode,
	AgentSetup,
	Alert,
	AlertList,
	AlertNote,
	Check,
	CheckHistory,
	CheckInput,
	CheckResult,
	DeviceHistory,
	DeviceSettings,
	DeviceUsage,
	ErrorBody,
	EventKind,
	EventList,
	Health,
	HealthHistory,
	Me,
	RecentResults,
	SetDeviceSettings,
	UsageHistory
} from './types.js';

/**
 * Roost refuses requests that change things unless they carry this header,
 * which a page on another site can't add. It matches CSRFHeader in
 * internal/api/auth.go.
 */
const csrfHeaders = { 'x-roost': '1' };

/** An error response from the Roost API. */
export class ApiError extends Error {
	readonly status: number;

	constructor(status: number, message: string) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
	}
}

/** The global fetch, or the one SvelteKit passes to a `load` function. */
type Fetch = typeof fetch;

/**
 * Sends a request to /api/{path} with an optional JSON body, and returns the
 * JSON response. It throws an ApiError, with the hub's message, if the
 * response is an error.
 */
export async function request<T>(
	method: string,
	path: string,
	body?: unknown,
	fetcher: Fetch = fetch
): Promise<T> {
	const headers: Record<string, string> = { accept: 'application/json', ...csrfHeaders };
	if (body !== undefined) {
		headers['content-type'] = 'application/json';
	}
	const res = await fetcher(`/api/${path}`, {
		method,
		headers,
		body: body === undefined ? undefined : JSON.stringify(body)
	});
	if (!res.ok) {
		const err: Partial<ErrorBody> = await res.json().catch(() => ({}));
		throw new ApiError(res.status, err.error ?? `${res.status} ${res.statusText}`);
	}
	return res.status === 204 ? (undefined as T) : res.json();
}

/**
 * Sends a request that changes something, for a button or form. If it
 * fails, a toast says why and the error is thrown on. If it works and
 * `success` is given, a toast says that.
 */
export async function mutate<T>(
	method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
	path: string,
	body?: unknown,
	{ success }: { success?: string } = {}
): Promise<T> {
	try {
		const out = await request<T>(method, path, body);
		if (success) {
			toast(success);
		}
		return out;
	} catch (err) {
		toast(err instanceof Error ? err.message : String(err), 'error');
		throw err;
	}
}

export function getHealth(): Promise<Health> {
	return request('GET', 'health');
}

/**
 * Who Roost thinks you are, from the device you're on. From a `load`
 * function, pass the `fetch` it's given.
 */
export function getMe(fetcher?: Fetch): Promise<Me> {
	return request('GET', 'me', undefined, fetcher);
}

/** How far back a device's history goes. */
export type HistoryRange = '1h' | '6h' | '24h' | '7d';

/** A device's metrics, and its round-trip times from the hub, over the last range. */
export function getDeviceHistory(id: string, range: HistoryRange): Promise<DeviceHistory> {
	return request('GET', `devices/${encodeURIComponent(id)}/history?range=${range}`);
}

/**
 * What uses a device's CPU and memory now, or with `at`, in the stored
 * minute nearest that time in Unix milliseconds.
 */
export function getDeviceUsage(id: string, at?: number): Promise<DeviceUsage> {
	const query = at === undefined ? '' : `?at=${Math.round(at)}`;
	return request('GET', `devices/${encodeURIComponent(id)}/usage${query}`);
}

/** Which groups of processes used a device's CPU and memory over the last range. */
export function getUsageHistory(id: string, range: HistoryRange): Promise<UsageHistory> {
	return request('GET', `devices/${encodeURIComponent(id)}/usage/history?range=${range}`);
}

/** The agent's version and the command that removes it. Admins only. */
export function getAgentSetup(): Promise<AgentSetup> {
	return request('GET', 'agent');
}

/**
 * A new command that installs the agent on one machine, with an enrollment
 * code in it. Admins only.
 */
export function newAgentCode(): Promise<AgentCode> {
	return request('POST', 'agent/codes');
}

/**
 * Revokes a device's agent, so Roost refuses its heartbeats and forgets what
 * it reported, and says so in a toast. Admins only.
 */
export function revokeAgent(id: string): Promise<void> {
	return mutate('DELETE', `devices/${encodeURIComponent(id)}/agent`, undefined, {
		success: 'Agent revoked'
	});
}

/** Replaces a device's settings, and says so in a toast. Admins only. */
export function putDeviceSettings(
	id: string,
	settings: SetDeviceSettings
): Promise<DeviceSettings> {
	return mutate('PUT', `devices/${encodeURIComponent(id)}/settings`, settings, {
		success: 'Saved'
	});
}

function checkPath(id: string): string {
	return `checks/${encodeURIComponent(id)}`;
}

/** The latest results of every check. */
export function getRecentResults(): Promise<RecentResults> {
	return request('GET', 'checks/recent');
}

/** A check's latency and uptime over the last range. */
export function getCheckHistory(id: string, range: HistoryRange): Promise<CheckHistory> {
	return request('GET', `${checkPath(id)}/history?range=${range}`);
}

/** The health score over the last range. */
export function getHealthHistory(range: HistoryRange): Promise<HealthHistory> {
	return request('GET', `health/history?range=${range}`);
}

/** Adds a check, and says so in a toast. Admins only. */
export function addCheck(input: CheckInput): Promise<Check> {
	return mutate('POST', 'checks', input, { success: 'Check added' });
}

/** Replaces a check, and says so in a toast. Admins only. */
export function putCheck(id: string, input: CheckInput, success = 'Saved'): Promise<Check> {
	return mutate('PUT', checkPath(id), input, { success });
}

/** Deletes a check and its history, and says so in a toast. Admins only. */
export function deleteCheck(id: string): Promise<void> {
	return mutate('DELETE', checkPath(id), undefined, { success: 'Check deleted' });
}

/** Runs a check now and returns its result. Admins only. */
export function runCheck(id: string): Promise<CheckResult> {
	return mutate('POST', `${checkPath(id)}/run`);
}

/** Resolved alerts, the latest opened first, older than the alert `before` if it's given. */
export function getResolvedAlerts(before?: number): Promise<AlertList> {
	return request('GET', `alerts${before ? `?before=${before}` : ''}`);
}

function alertPath(id: number): string {
	return `alerts/${encodeURIComponent(String(id))}`;
}

/** Acknowledges an open alert, or with `ack` false, takes that back. Admins only. */
export function ackAlert(id: number, ack: boolean): Promise<Alert> {
	return mutate(ack ? 'PUT' : 'DELETE', `${alertPath(id)}/ack`);
}

/** Snoozes an open alert for some minutes, or with null, wakes it. Admins only. */
export function snoozeAlert(id: number, minutes: number | null): Promise<Alert> {
	return minutes === null
		? mutate('DELETE', `${alertPath(id)}/snooze`)
		: mutate('PUT', `${alertPath(id)}/snooze`, { minutes });
}

/** Adds a note to an alert. Admins only. */
export function addAlertNote(id: number, text: string): Promise<AlertNote> {
	return mutate('POST', `${alertPath(id)}/notes`, { text });
}

/** What to read of the activity log. */
export interface EventQuery {
	/** Events older than this one. */
	before?: number;
	/** Events newer than this one. */
	after?: number;
	kind?: EventKind;
	deviceId?: string;
	limit?: number;
}

/** A page of the activity log, newest first. */
export function getEvents(q: EventQuery = {}): Promise<EventList> {
	const params = new URLSearchParams();
	if (q.before) params.set('before', String(q.before));
	if (q.after) params.set('after', String(q.after));
	if (q.kind) params.set('kind', q.kind);
	if (q.deviceId) params.set('device', q.deviceId);
	if (q.limit) params.set('limit', String(q.limit));
	const query = params.toString();
	return request('GET', `events${query ? `?${query}` : ''}`);
}
