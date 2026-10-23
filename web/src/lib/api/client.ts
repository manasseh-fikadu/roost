import { toast } from '#lib/toast.svelte.js';
import type {
	AgentCode,
	AgentSetup,
	DeviceHistory,
	DeviceSettings,
	DeviceUsage,
	ErrorBody,
	Health,
	Me,
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
