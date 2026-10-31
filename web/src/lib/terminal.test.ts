import { describe, expect, test } from 'bun:test';
import type { Device, DeviceSSH } from './api/types.js';
import {
	Acker,
	canOpen,
	exitMessage,
	listedDeviceId,
	loadTabs,
	nextSessionName,
	parsePort,
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
		docker: false,
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

describe('listedDeviceId', () => {
	const embedded = [device('roost', hub, { self: true }), device('vps', hub), device('laptop', ok)];
	test.each<{ name: string; devices: Device[]; id: string; want: string }>([
		{ name: "the machine's own node stays", devices: embedded, id: 'vps', want: 'vps' },
		{
			name: "Roost's own node means the machine's, in embedded mode",
			devices: embedded,
			id: 'roost',
			want: 'vps'
		},
		{ name: 'another device stays', devices: embedded, id: 'laptop', want: 'laptop' },
		{
			name: "Roost's own node stays when it's the only one",
			devices: [device('roost', hub, { self: true }), device('laptop', ok)],
			id: 'roost',
			want: 'roost'
		},
		{
			name: "a device that isn't listed stays",
			devices: [...embedded, device('phone')],
			id: 'phone',
			want: 'phone'
		},
		{ name: "one that isn't there stays", devices: embedded, id: 'gone', want: 'gone' }
	])('$name', ({ devices, id, want }) => {
		expect(listedDeviceId(devices, id)).toBe(want);
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

	test("a tab on Roost's own node follows the machine's node, in embedded mode", () => {
		const embedded = [device('roost', hub, { self: true }), device('vps', hub)];
		const saved = JSON.stringify([{ key: 'a', deviceId: 'roost', session: 'web-1' }]);
		expect(loadTabs(saved, embedded)).toEqual([
			{ key: 'a', deviceId: 'vps', session: 'web-1', cwd: null }
		]);
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

describe('exitMessage', () => {
	test.each<{
		name: string;
		session: string | null;
		code: number | null | undefined;
		want: string;
	}>([
		{
			name: 'a dropped connection on a tmux tab: the session may still run',
			session: 'web-1',
			code: undefined,
			want: 'Lost the connection to web-1. It may still be running on the machine.'
		},
		{
			name: 'a null status, as the same',
			session: 'web-1',
			code: null,
			want: 'Lost the connection to web-1. It may still be running on the machine.'
		},
		{
			name: 'a dropped connection on a shell',
			session: null,
			code: undefined,
			want: 'Lost the connection to the shell.'
		},
		{
			name: 'tmux exits with 0 after a detach, which looks like the end of the session',
			session: 'web-1',
			code: 0,
			want: 'Detached from web-1, or the session ended.'
		},
		{ name: 'tmux fails', session: 'web-1', code: 1, want: 'tmux exited with status 1.' },
		{ name: 'no tmux', session: 'web-1', code: 127, want: "tmux isn't installed on the machine." },
		{ name: 'a shell exits', session: null, code: 0, want: 'The shell exited.' },
		{
			name: 'a shell exits with a status',
			session: null,
			code: 3,
			want: 'The shell exited with status 3.'
		}
	])('$name', ({ session, code, want }) => {
		expect(exitMessage(session, code)).toBe(want);
	});
});

describe('parsePort', () => {
	test.each<{ text: string; want: number | null }>([
		{ text: '', want: 22 },
		{ text: '   ', want: 22 },
		{ text: '22', want: 22 },
		{ text: ' 2222 ', want: 2222 },
		{ text: '1', want: 1 },
		{ text: '65535', want: 65535 },
		{ text: '22a', want: null },
		{ text: 'ssh', want: null },
		{ text: '0', want: null },
		{ text: '65536', want: null },
		{ text: '-1', want: null },
		{ text: '22.5', want: null },
		{ text: '1e3', want: null },
		{ text: '0x16', want: null },
		{ text: '٢٢', want: null },
		{ text: '123456', want: null }
	])('"$text"', ({ text, want }) => {
		expect(parsePort(text)).toBe(want);
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
