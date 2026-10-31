import { describe, expect, test } from 'bun:test';
import { agentRuns, agentState } from './agent.js';
import type { Agent, Device, MeasuredBy } from './api/types.js';
import type { Status } from './status.js';

const now = Date.parse('2026-10-23T12:00:00Z');
const minutesAgo = (m: number) => new Date(now - m * 60_000).toISOString();

function device(
	os: string,
	agent?: Agent,
	{ online = true, measuredBy = undefined as MeasuredBy | undefined } = {}
): Device {
	return {
		id: 'laptop',
		name: 'laptop',
		hostName: 'laptop',
		dnsName: 'laptop.example.ts.net',
		os,
		kind: 'laptop',
		ips: ['100.64.0.2'],
		online,
		status: online ? 'online' : 'offline',
		statusReason: '',
		path: { kind: online ? 'direct' : 'offline' },
		rxBytes: 0,
		txBytes: 0,
		measuredBy: measuredBy ?? (agent?.lastSeen ? 'agent' : undefined),
		agent,
		docker: false,
		settings: { displayName: '', alertOffline: true, notes: '' }
	};
}

const reporting: Agent = { version: '1.3.0', lastSeen: minutesAgo(0), enrolledAt: minutesAgo(60) };

describe('agentState', () => {
	test.each<{ name: string; device: Device; label: string; status: Status; detail?: string }>([
		{
			name: 'the hub',
			device: device('linux', undefined, { measuredBy: 'hub' }),
			label: 'Built in',
			status: 'online',
			detail: 'Roost runs here and measures it'
		},
		{ name: 'no agent', device: device('macOS'), label: 'Not installed', status: 'unknown' },
		{
			name: 'an OS the agent skips',
			device: device('iOS'),
			label: 'Not supported',
			status: 'unknown'
		},
		{
			name: 'enrolled, before its first heartbeat',
			device: device('linux', { enrolledAt: minutesAgo(2) }),
			label: 'Waiting for a heartbeat',
			status: 'unknown',
			detail: 'enrolled 2m ago'
		},
		{
			name: 'reporting',
			device: device('linux', reporting),
			label: 'Reporting',
			status: 'online',
			detail: 'v1.3.0 · heartbeat just now'
		},
		{
			name: 'an old version',
			device: device('linux', { ...reporting, version: '1.2.0', outdated: true }),
			label: 'Update available',
			status: 'attention',
			detail: 'v1.2.0 · heartbeat just now'
		},
		{
			name: 'stopped',
			device: device('linux', { ...reporting, lastSeen: minutesAgo(5), stale: true }),
			label: 'Stopped reporting',
			status: 'attention',
			detail: 'last heartbeat 5m ago'
		},
		{
			name: 'offline',
			device: device('linux', { ...reporting, lastSeen: minutesAgo(90) }, { online: false }),
			label: 'Offline',
			status: 'unknown',
			detail: 'last heartbeat 1h ago'
		},
		{
			name: 'from before enrollment',
			device: device('linux', {
				version: '1.2.0',
				lastSeen: minutesAgo(5),
				stale: true,
				outdated: true
			}),
			label: 'Not enrolled',
			status: 'attention',
			detail: 'v1.2.0 · last heartbeat 5m ago'
		}
	])('$name', ({ device, label, status, detail }) => {
		expect(agentState(device, now)).toEqual({ label, status, detail });
	});
});

describe('agentRuns', () => {
	test.each([
		{ os: 'linux', want: true },
		{ os: 'macOS', want: true },
		{ os: 'windows', want: false },
		{ os: '', want: false }
	])('$os', ({ os, want }) => {
		expect(agentRuns(os)).toBe(want);
	});
});
