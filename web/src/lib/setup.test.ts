import { describe, expect, test } from 'bun:test';
import type { Collector, Device, Me, Snapshot } from './api/types.js';
import {
	dockerDetail,
	dockerHint,
	nextStep,
	setupChecks,
	shouldOpenSetup,
	sshCandidate,
	stepOf
} from './setup.js';

function device(id: string, self = false): Device {
	return { id, self } as Device;
}

function ok(name: string): Collector {
	return { name, state: 'ok' };
}

function snapshot(over: Partial<Snapshot> = {}): Snapshot {
	return {
		hub: { name: 'Roost', setupDone: false, tailnet: 'example.com' },
		collectors: [ok('Tailscale status'), ok('Docker'), ok('systemd units')],
		devices: [device('roost', true), device('laptop'), device('server')],
		containers: [{}, {}],
		units: [{}, {}, {}],
		...over
	} as Snapshot;
}

const alice: Me = {
	signedIn: true,
	login: 'alice@example.com',
	name: 'Alice',
	device: 'laptop.example.ts.net',
	role: 'admin'
};

function byId(checks: ReturnType<typeof setupChecks>) {
	return Object.fromEntries(checks.map((c) => [c.id, c]));
}

describe('setupChecks', () => {
	test('everything works', () => {
		const c = byId(setupChecks(snapshot(), alice, 'embedded'));
		expect(c.tailscale).toMatchObject({
			status: 'online',
			detail:
				'Roost is its own machine on the tailnet · tailnet example.com · 2 other devices found'
		});
		expect(c.signin).toMatchObject({
			status: 'online',
			detail: 'Alice (alice@example.com) on laptop · Admin'
		});
		expect(c.docker).toMatchObject({
			status: 'online',
			detail: 'Roost can read Docker · 2 containers'
		});
		expect(c.systemd).toMatchObject({
			status: 'online',
			detail: 'Roost can read systemd · 3 units watched'
		});
		expect(Object.values(c).every((x) => x.hint === undefined)).toBe(true);
	});

	test('the checks come in order', () => {
		expect(setupChecks(snapshot(), alice, 'host').map((c) => c.id)).toEqual([
			'tailscale',
			'signin',
			'docker',
			'systemd'
		]);
	});

	test('before the first snapshot', () => {
		for (const c of setupChecks(null, alice, undefined)) {
			if (c.id !== 'signin') {
				expect(c).toMatchObject({ status: 'unknown', detail: 'Checking…' });
			}
		}
	});

	test('host mode says whose Tailscale it is', () => {
		const t = byId(setupChecks(snapshot(), alice, 'host')).tailscale;
		expect(t.detail).toStartWith("Using this machine's Tailscale · ");
	});

	test('no tailnet name for a viewer, and one device found', () => {
		const s = snapshot({
			hub: { name: 'Roost', setupDone: false, tailnet: '' } as Snapshot['hub'],
			devices: [device('roost', true), device('laptop')]
		});
		expect(byId(setupChecks(s, alice, 'embedded')).tailscale.detail).toBe(
			'Roost is its own machine on the tailnet · 1 other device found'
		);
	});

	test('a tailnet with only Roost on it needs a hint', () => {
		const s = snapshot({ devices: [device('roost', true)] });
		const t = byId(setupChecks(s, alice, 'embedded')).tailscale;
		expect(t.status).toBe('attention');
		expect(t.detail).toEndWith('0 other devices found');
		expect(t.hint).toContain('Install Tailscale on one');
	});

	test.each<{ mode: 'host' | 'embedded'; hint: string }>([
		{ mode: 'host', hint: 'tailscale set --operator' },
		{ mode: 'embedded', hint: 'log it in again' }
	])('Tailscale failing in $mode mode', ({ mode, hint }) => {
		const s = snapshot({
			collectors: [{ name: 'Tailscale status', state: 'failing', error: 'no socket' }]
		});
		const t = byId(setupChecks(s, alice, mode)).tailscale;
		expect(t).toMatchObject({
			status: 'attention',
			detail: "Roost can't read the tailnet: no socket"
		});
		expect(t.hint).toContain(hint);
	});

	test('a collector that has not run yet is not a problem', () => {
		const s = snapshot({
			collectors: [
				{ name: 'Tailscale status', state: 'waiting' },
				{ name: 'Docker', state: 'waiting' },
				{ name: 'systemd units', state: 'waiting' }
			]
		});
		for (const c of setupChecks(s, alice, 'host')) {
			if (c.id !== 'signin') {
				expect(c.status).toBe('unknown');
			}
		}
	});

	test('no Docker and no systemd on the machine', () => {
		const s = snapshot({ collectors: [ok('Tailscale status')] });
		const c = byId(setupChecks(s, alice, 'host'));
		expect(c.docker).toMatchObject({
			status: 'unknown',
			detail: expect.stringContaining('No Docker socket')
		});
		expect(c.systemd).toMatchObject({
			status: 'unknown',
			detail: expect.stringContaining("doesn't run systemd")
		});
		expect(c.docker.hint).toBeDefined();
		expect(c.systemd.hint).toBeDefined();
	});

	test('Docker and systemd failing', () => {
		const err =
			"list the containers: can't reach Docker: Roost can't use /var/run/docker.sock; add the user Roost runs as to the docker group, and restart Roost";
		const s = snapshot({
			collectors: [
				ok('Tailscale status'),
				{ name: 'Docker', state: 'failing', error: err },
				{ name: 'systemd units', state: 'failing', error: 'exit status 1' }
			]
		});
		const c = byId(setupChecks(s, alice, 'host'));
		expect(c.docker).toMatchObject({
			status: 'attention',
			detail: "Roost can't open the Docker socket: permission denied."
		});
		expect(c.docker.hint).toContain('usermod -aG docker');
		expect(c.systemd).toMatchObject({ status: 'attention', detail: 'exit status 1' });
		expect(c.systemd.hint).toContain('systemctl');
	});

	test('someone who is not signed in', () => {
		const c = byId(setupChecks(snapshot(), { signedIn: false }, 'host')).signin;
		expect(c.status).toBe('attention');
		expect(c.hint).toBeDefined();
	});

	test('a login with no display name', () => {
		const c = byId(
			setupChecks(snapshot(), { signedIn: true, login: 'bob@example.com', role: 'admin' }, 'host')
		);
		expect(c.signin.detail).toBe('bob@example.com · Admin');
	});
});

describe('dockerDetail', () => {
	test.each([
		['Roost can’t use /x; add the user Roost runs as to the docker group', 'permission denied'],
		['there’s no socket at /var/run/docker.sock', 'no Docker socket'],
		['nothing listens on /var/run/docker.sock', 'nothing answers'],
		['dial failed: timeout', 'dial failed: timeout'],
		['', "can't read Docker"]
	])('%s', (error, want) => {
		expect(dockerDetail(error)).toContain(want);
	});
});

describe('dockerHint', () => {
	test.each([
		['Roost can’t use /x; add the user Roost runs as to the docker group', 'usermod'],
		['there’s no socket at /var/run/docker.sock', 'isn’t installed'],
		['nothing listens on /var/run/docker.sock', 'systemctl start docker'],
		['something else', 'Make sure Docker runs']
	])('%s', (error, want) => {
		expect(dockerHint(error)).toContain(want);
	});
});

describe('steps', () => {
	test.each([
		{ param: null, want: 'checks' },
		{ param: 'name', want: 'name' },
		{ param: 'connect', want: 'connect' },
		{ param: 'nope', want: 'checks' },
		{ param: '', want: 'checks' }
	] as const)('stepOf($param)', ({ param, want }) => {
		expect(stepOf(param)).toBe(want);
	});

	test('nextStep', () => {
		expect(nextStep('checks')).toBe('name');
		expect(nextStep('name')).toBe('connect');
		expect(nextStep('connect')).toBeNull();
	});
});

describe('shouldOpenSetup', () => {
	test.each<{
		name: string;
		admin: boolean;
		hub?: { setupDone: boolean };
		sent: boolean;
		want: boolean;
	}>([
		{
			name: 'an admin of a new Roost',
			admin: true,
			hub: { setupDone: false },
			sent: false,
			want: true
		},
		{
			name: 'once is enough in a tab',
			admin: true,
			hub: { setupDone: false },
			sent: true,
			want: false
		},
		{ name: 'done', admin: true, hub: { setupDone: true }, sent: false, want: false },
		{ name: 'a viewer', admin: false, hub: { setupDone: false }, sent: false, want: false },
		{ name: 'no snapshot yet', admin: true, hub: undefined, sent: false, want: false }
	])('$name', ({ admin, hub, sent, want }) => {
		expect(shouldOpenSetup(admin, hub, sent)).toBe(want);
	});
});

describe('sshCandidate', () => {
	const dev = (id: string, online: boolean, state?: 'not-set-up' | 'ok' | 'failing') =>
		({ id, online, ssh: state && { state } }) as Device;

	test('the first online device without access', () => {
		const devices = [
			dev('phone', true), // no sshd to log in to
			dev('off', false, 'not-set-up'),
			dev('done', true, 'ok'),
			dev('broken', true, 'failing'),
			dev('next', true, 'not-set-up'),
			dev('later', true, 'not-set-up')
		];
		expect(sshCandidate(devices)?.id).toBe('next');
	});

	test('nothing left to set up', () => {
		expect(sshCandidate([dev('a', true, 'ok'), dev('b', false, 'not-set-up')])).toBeUndefined();
		expect(sshCandidate([])).toBeUndefined();
	});
});
