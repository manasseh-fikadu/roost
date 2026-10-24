import { describe, expect, test } from 'bun:test';
import type { Device, DeviceSSH } from './api/types.js';
import {
	Acker,
	canOpen,
	loadTabs,
	nextSessionName,
	saveTabs,
	terminalDevices,
	terminalURL,
	type TerminalTab
} from './terminal.js';

function device(id: string, ssh?: DeviceSSH, { online = true, self = false } = {}): Device {
	return {
		id,
		name: id,
		hostName: id,
		dnsName: `${id}.example.ts.net`,
		os: 'linux',
		kind: 'server',
		ips: ['100.64.0.2'],
		online,
		self,
		status: online ? 'online' : 'offline',
		statusReason: '',
		path: { kind: online ? 'direct' : 'offline' },
		rxBytes: 0,
		txBytes: 0,
		settings: { displayName: '', alertOffline: true, notes: '' },
		ssh
	};
}

const ok: DeviceSSH = { state: 'ok', user: 'alice', port: 22 };
const hub: DeviceSSH = { ...ok, hub: true };

describe('terminalDevices', () => {
	test.each<{ name: string; devices: Device[]; want: string[] }>([
		{
			name: 'set up only',
			devices: [
				device('laptop', ok),
				device('phone'),
				device('new', { state: 'not-set-up' }),
				device('broken', { ...ok, state: 'failing' })
			],
			want: ['laptop', 'broken']
		},
		{
			name: "the hub's own node, in host mode",
			devices: [device('roost', hub, { self: true }), device('laptop', ok)],
			want: ['roost', 'laptop']
		},
		{
			name: "the machine's node, not Roost's, in embedded mode",
			devices: [device('roost', hub, { self: true }), device('vps', hub)],
			want: ['vps']
		}
	])('$name', ({ devices, want }) => {
		expect(terminalDevices(devices).map((d) => d.id)).toEqual(want);
	});
});

describe('canOpen', () => {
	test.each<{ name: string; device: Device; want: boolean }>([
		{ name: 'connected', device: device('laptop', ok), want: true },
		{
			name: 'not checked yet',
			device: device('laptop', { ...ok, state: 'unchecked' }),
			want: true
		},
		{
			name: 'failing, to see why',
			device: device('laptop', { ...ok, state: 'failing' }),
			want: true
		},
		{ name: 'offline', device: device('laptop', ok, { online: false }), want: false },
		{
			name: 'the hub, whatever Tailscale says',
			device: device('roost', hub, { online: false }),
			want: true
		},
		{
			name: 'host key changed',
			device: device('laptop', { ...ok, state: 'host-key-changed' }),
			want: false
		},
		{ name: 'not set up', device: device('laptop', { state: 'not-set-up' }), want: false },
		{ name: 'no SSH', device: device('phone'), want: false }
	])('$name', ({ device, want }) => {
		expect(canOpen(device)).toBe(want);
	});
});

describe('loadTabs', () => {
	const devices = [device('laptop', ok), device('phone')];
	test.each<{ name: string; saved: string | null; want: TerminalTab[] }>([
		{ name: 'nothing saved', saved: null, want: [] },
		{ name: 'not JSON', saved: '{', want: [] },
		{ name: 'not a list', saved: '{"key":"a"}', want: [] },
		{
			name: 'tmux tabs on devices with access',
			saved: JSON.stringify([
				{ key: 'a', deviceId: 'laptop', session: 'web-1', cwd: '/srv' },
				{ key: 'b', deviceId: 'laptop', session: null },
				{ key: 'c', deviceId: 'phone', session: 'web-1' },
				{ key: 'd', deviceId: 'gone', session: 'web-1' },
				{ key: 'a', deviceId: 'laptop', session: 'web-2' },
				null,
				'web-3'
			]),
			// The directory only applies when a session is made.
			want: [{ key: 'a', deviceId: 'laptop', session: 'web-1', cwd: null }]
		}
	])('$name', ({ saved, want }) => {
		expect(loadTabs(saved, devices)).toEqual(want);
	});

	test('saveTabs keeps the tmux tabs', () => {
		const tabs: TerminalTab[] = [
			{ key: 'a', deviceId: 'laptop', session: 'web-1', cwd: '/srv' },
			{ key: 'b', deviceId: 'laptop', session: null, cwd: null }
		];
		expect(loadTabs(saveTabs(tabs), devices)).toEqual([{ ...tabs[0], cwd: null }]);
	});
});

describe('nextSessionName', () => {
	test.each<{ taken: string[]; want: string }>([
		{ taken: [], want: 'web-1' },
		{ taken: ['web-1', 'main'], want: 'web-2' },
		{ taken: ['web-2', 'web-1', 'web-4'], want: 'web-3' }
	])('after $taken', ({ taken, want }) => {
		expect(nextSessionName(taken)).toBe(want);
	});
});

describe('terminalURL', () => {
	test.each<{
		name: string;
		protocol: string;
		session: string | null;
		cwd: string | null;
		want: string;
	}>([
		{
			name: 'a plain shell over HTTPS',
			protocol: 'https:',
			session: null,
			cwd: null,
			want: 'wss://roost.example.ts.net/api/devices/n%2F1/terminal?cols=80&rows=24'
		},
		{
			name: 'a tmux session in a directory over HTTP',
			protocol: 'http:',
			session: "it's",
			cwd: '/srv/my app',
			want: 'ws://roost.example.ts.net/api/devices/n%2F1/terminal?cols=80&rows=24&session=it%27s&cwd=%2Fsrv%2Fmy+app'
		}
	])('$name', ({ protocol, session, cwd, want }) => {
		const url = terminalURL({ protocol, host: 'roost.example.ts.net' }, 'n/1', {
			session,
			cwd,
			cols: 80,
			rows: 24
		});
		expect(url).toBe(want);
		expect(new URL(url).searchParams.get('session')).toBe(session);
		expect(new URL(url).searchParams.get('cwd')).toBe(cwd);
	});
});

describe('Acker', () => {
	test('acks every so often, and flushes the rest', () => {
		const a = new Acker(100);
		expect([a.wrote(40), a.wrote(40), a.wrote(40), a.wrote(10)]).toEqual([0, 0, 120, 0]);
		expect(a.flush()).toBe(10);
		expect(a.flush()).toBe(0);
	});
});
