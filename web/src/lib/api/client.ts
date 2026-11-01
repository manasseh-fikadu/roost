import { toast, type ToastAction } from '#lib/toast.svelte.js';
import type {
	Access,
	AgentCode,
	AgentRun,
	AgentSetup,
	Alert,
	AlertList,
	AlertNote,
	Check,
	CheckHistory,
	CheckInput,
	CheckResult,
	ChangeSettings,
	ChannelDelivery,
	ChannelInput,
	DeviceHistory,
	DeviceSettings,
	DeviceSSH,
	DeviceUsage,
	ErrorBody,
	EventKind,
	EventList,
	Health,
	HealthHistory,
	InstalledServices,
	Me,
	NotificationChannel,
	NotificationChannels,
	RecentResults,
	Role,
	SetDeviceSettings,
	SettingsView,
	SetSSH,
	SSHSetup,
	TerminalSessions,
	UsageHistory,
	WhoCanView
} from './types.js';

/**
 * Roost refuses requests that change things unless they carry this header,
 * which a page on another site can't add. It matches CSRFHeader in
 * internal/api/auth.go.
 */
export const csrfHeaders = { 'x-roost': '1' };

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
 * `success` is given, a toast says that, with `undo` as a button if there is
 * one that takes it back.
 */
export async function mutate<T>(
	method: 'POST' | 'PUT' | 'PATCH' | 'DELETE',
	path: string,
	body?: unknown,
	{ success, undo }: { success?: string; undo?: ToastAction } = {}
): Promise<T> {
	try {
		const out = await request<T>(method, path, body);
		if (success) {
			toast(success, 'default', undo ? { action: undo, duration: 6000 } : {});
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

/**
 * Installs the agent on a device over SSH, as the user Roost logs in as,
 * with a new enrollment code. A script that fails is an answer with `ok`
 * false, not an error. Admins only.
 */
export function installAgentOverSSH(id: string): Promise<AgentRun> {
	return mutate('POST', `devices/${encodeURIComponent(id)}/agent/install`);
}

/**
 * Removes the agent from a device over SSH, for the user Roost logs in as.
 * Admins only.
 */
export function uninstallAgentOverSSH(id: string): Promise<AgentRun> {
	return mutate('POST', `devices/${encodeURIComponent(id)}/agent/uninstall`);
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

function sshPath(id: string, rest = ''): string {
	return `devices/${encodeURIComponent(id)}/ssh${rest}`;
}

/** How to let Roost log in to a device over SSH. Admins only. */
export function getSSHSetup(id: string): Promise<SSHSetup> {
	return request('GET', sshPath(id));
}

/** Sets up how Roost logs in to a device, and says so in a toast. Admins only. */
export function putSSH(id: string, body: SetSSH): Promise<DeviceSSH> {
	return mutate('PUT', sshPath(id), body, { success: 'Saved' });
}

/**
 * Adds Roost's key for the user it runs as, on the machine it runs on, and
 * sets up access with it. Admins only.
 */
export function authorizeSSH(id: string): Promise<DeviceSSH> {
	return mutate('POST', sshPath(id, '/authorize'), undefined, { success: 'Roost can log in here' });
}

/** Logs in to a device afresh and finds out what it offers. Admins only. */
export function checkSSH(id: string): Promise<DeviceSSH> {
	return mutate('POST', sshPath(id, '/check'));
}

/**
 * Pins the host key a device offered in place of the one Roost pinned, and
 * says so in a toast. `fingerprint` is the offered key's, as the admin saw
 * it: if the device offers another by now, the hub refuses. Admins only.
 */
export function trustHostKey(id: string, fingerprint: string): Promise<DeviceSSH> {
	return mutate('PUT', sshPath(id, '/host-key'), { fingerprint }, { success: 'Host key trusted' });
}

/**
 * Forgets a device's pinned host key, so Roost pins whichever key it offers
 * next, and says so in a toast. Admins only.
 */
export function resetHostKey(id: string): Promise<DeviceSSH> {
	return mutate('DELETE', sshPath(id, '/host-key'), undefined, {
		success: 'Host key forgotten'
	});
}

/** Removes Roost's SSH access to a device, and says so in a toast. Admins only. */
export function removeSSH(id: string): Promise<void> {
	return mutate('DELETE', sshPath(id), undefined, { success: 'SSH access removed' });
}

/** The tmux sessions on a device. Admins only. */
export function getTerminalSessions(id: string): Promise<TerminalSessions> {
	return request('GET', `devices/${encodeURIComponent(id)}/terminal/sessions`);
}

/** Ends a tmux session and what runs in it, and says so in a toast. Admins only. */
export function killTerminalSession(id: string, name: string): Promise<void> {
	return mutate(
		'DELETE',
		`devices/${encodeURIComponent(id)}/terminal/sessions/${encodeURIComponent(name)}`,
		undefined,
		{ success: `Ended ${name}` }
	);
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

/** An alert, open or resolved. */
export function getAlert(id: number): Promise<Alert> {
	return request('GET', `alerts/${encodeURIComponent(String(id))}`);
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

/** Roost's settings, with the defaults and how Roost joined the tailnet. Admins only. */
export function getSettings(): Promise<SettingsView> {
	return request('GET', 'settings');
}

/**
 * Replaces the parts of the settings that `change` has and leaves the rest,
 * and says so in a toast. Roost uses the change at once. Admins only.
 */
export function changeSettings(change: ChangeSettings, success = 'Saved'): Promise<SettingsView> {
	return mutate('PATCH', 'settings', change, { success });
}

/** The services on the machine Roost runs on, to pick the ones to watch from. Admins only. */
export function getInstalledServices(): Promise<InstalledServices> {
	return request('GET', 'settings/units');
}

/** Who has a role in Roost, and who else can view it. Admins only. */
export function getAccess(): Promise<Access> {
	return request('GET', 'access');
}

/** Gives a Tailscale login a role, or changes it. Admins only. */
export function setRole(login: string, role: Role, success = 'Saved'): Promise<Access> {
	return mutate('PUT', `access/users/${encodeURIComponent(login)}`, { role }, { success });
}

/**
 * Takes a login's role away, and says so in a toast, with `undo` as its
 * button. Admins only.
 */
export function removeUser(login: string, undo?: ToastAction): Promise<Access> {
	return mutate('DELETE', `access/users/${encodeURIComponent(login)}`, undefined, {
		success: 'Removed',
		undo
	});
}

/** Changes who can view Roost, other than the users with a role. Admins only. */
export function setWhoCanView(whoCanView: WhoCanView): Promise<Access> {
	return mutate('PATCH', 'access', { whoCanView }, { success: 'Saved' });
}

/** The notification channels, by name. Admins only. */
export function getChannels(): Promise<NotificationChannels> {
	return request('GET', 'notifications/channels');
}

function channelPath(id: number): string {
	return `notifications/channels/${encodeURIComponent(String(id))}`;
}

/** Adds a notification channel, and says so in a toast. Admins only. */
export function addChannel(input: ChannelInput): Promise<NotificationChannel> {
	return mutate('POST', 'notifications/channels', input, { success: 'Channel added' });
}

/** Replaces a notification channel, and says so in a toast. Admins only. */
export function putChannel(
	id: number,
	input: ChannelInput,
	success = 'Saved'
): Promise<NotificationChannel> {
	return mutate('PUT', channelPath(id), input, { success });
}

/** Deletes a notification channel, and says so in a toast. Admins only. */
export function deleteChannel(id: number): Promise<void> {
	return mutate('DELETE', channelPath(id), undefined, { success: 'Channel deleted' });
}

/**
 * Sends a test message to a channel and says how it went. A message that
 * didn't go isn't an error: the answer says why. Admins only.
 */
export function testChannel(id: number): Promise<ChannelDelivery> {
	return mutate('POST', `${channelPath(id)}/test`);
}
