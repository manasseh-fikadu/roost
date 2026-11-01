import { describe, expect, test } from 'bun:test';
import type { Alert, Check, Container, Device, Snapshot } from '#lib/api/types.js';
import { paletteActions } from './actions.js';

function device(over: Partial<Device> = {}): Device {
	return {
		id: 'd1',
		name: 'nas',
		dnsName: 'nas.example.ts.net',
		ips: ['100.64.0.1'],
		online: true,
		ssh: { state: 'ok' },
		...over
	} as Device;
}

function container(over: Partial<Container> = {}): Container {
	return {
		id: 'c1',
		name: 'web',
		image: 'nginx',
		state: 'running',
		deviceId: 'd1',
		...over
	} as Container;
}

function snapshot(over: Partial<Snapshot> = {}): Snapshot {
	return { devices: [], containers: [], checks: [], alerts: [], ...over } as unknown as Snapshot;
}

const ids = (s: Snapshot | null, admin: boolean) => paletteActions(s, admin).map((a) => a.id);

describe('paletteActions', () => {
	test('before the first snapshot there is only the shortcuts', () => {
		expect(ids(null, true)).toEqual(['shortcuts']);
	});

	test('a viewer gets Docker links but nothing that changes something', () => {
		const s = snapshot({
			devices: [device({ docker: true, ssh: undefined })],
			containers: [container()],
			checks: [{ id: 'k1', name: 'Home' } as Check]
		});
		expect(ids(s, false)).toEqual(['shortcuts', 'docker:d1']);
	});

	test('an admin can open a terminal and files on a machine with working SSH', () => {
		const s = snapshot({ devices: [device()] });
		expect(ids(s, true)).toEqual(['shortcuts', 'terminal:d1', 'files:d1', 'new-check']);
	});

	test('a machine without SSH access gets no terminal', () => {
		const s = snapshot({ devices: [device({ ssh: { state: 'not-set-up' } })] });
		expect(ids(s, true)).toEqual(['shortcuts', 'new-check']);
	});

	test.each([
		['running', ['restart', 'stop']],
		['exited', ['start']],
		['paused', ['stop']]
	])('a %s container offers %j', (state, actions) => {
		const s = snapshot({ devices: [device()], containers: [container({ state: state as never })] });
		const got = paletteActions(s, true).flatMap((a) => (a.kind === 'container' ? [a.action] : []));
		expect(got).toEqual(actions as never);
	});

	test('a container action is labeled with its verb and name, and knows its device', () => {
		const s = snapshot({ devices: [device()], containers: [container()] });
		const restart = paletteActions(s, true).find((a) => a.id === 'container:d1:c1:restart');
		expect(restart).toMatchObject({
			label: 'Restart web',
			hint: 'nginx',
			deviceId: 'd1',
			deviceName: 'nas',
			containerId: 'c1'
		});
	});

	test('only a check an admin made can be run, not a device ping', () => {
		const s = snapshot({
			checks: [
				{ id: 'k1', name: 'Home' },
				{ id: 'k2', name: 'Ping nas', deviceId: 'd1' }
			] as Check[]
		});
		expect(ids(s, true)).toContain('check:k1');
		expect(ids(s, true)).not.toContain('check:k2');
	});

	test('an alert is offered once, until somebody acknowledges it', () => {
		const alert = (over: Partial<Alert>) =>
			({ id: 1, title: 'Disk full', state: 'open', ...over }) as Alert;
		const open = snapshot({ alerts: [alert({})] });
		const acked = snapshot({
			alerts: [alert({ state: 'acknowledged', ackedAt: '2027-01-01T00:00:00Z' })]
		});
		const snoozed = snapshot({ alerts: [alert({ state: 'snoozed' })] });
		expect(ids(open, true)).toContain('alert:1');
		expect(ids(acked, true)).not.toContain('alert:1');
		expect(ids(snoozed, true)).not.toContain('alert:1');
	});
});
