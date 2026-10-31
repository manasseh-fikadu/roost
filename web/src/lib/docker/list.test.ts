import { describe, expect, test } from 'bun:test';
import type {
	DockerContainerRow,
	DockerDisk,
	DockerImage,
	DockerNetwork,
	DockerOverview,
	DockerVolume
} from '#lib/api/types.js';
import {
	cpuMeter,
	cpuShare,
	imageTotals,
	memShare,
	networkTotals,
	pacedOverview,
	parseTab,
	portChips,
	reclaimable,
	shortDigest,
	uptimeLabel,
	visibleRows,
	volumeName,
	volumeTotals
} from './list.js';

function row(over: Partial<DockerContainerRow> = {}): DockerContainerRow {
	return {
		id: 'a'.repeat(64),
		name: 'web',
		image: 'nginx:1.27',
		imageId: 'sha256:1',
		created: '2027-01-01T00:00:00Z',
		state: 'running',
		status: 'Up 2 hours',
		restartCount: 0,
		ports: [],
		networks: [],
		mounts: 0,
		memLimitSet: false,
		stats: null,
		...over
	};
}

describe('parseTab', () => {
	test.each([
		{ value: null, want: 'containers' },
		{ value: '', want: 'containers' },
		{ value: 'images', want: 'images' },
		{ value: 'networks', want: 'networks' },
		{ value: 'logs', want: 'containers' }
	])('$value', ({ value, want }) => {
		expect(parseTab(value)).toBe(want as ReturnType<typeof parseTab>);
	});
});

describe('visibleRows', () => {
	const rows = [
		row({ name: 'api', state: 'running' }),
		row({ name: 'worker', state: 'restarting' }),
		row({ name: 'pg', state: 'paused' }),
		row({ name: 'old', state: 'exited' }),
		row({ name: 'new', state: 'created' })
	];
	const names = (q: string, showStopped: boolean) =>
		visibleRows(rows, q, showStopped).map((c) => c.name);

	const cases: { name: string; q: string; stopped: boolean; want: string[] }[] = [
		{ name: 'all', q: '', stopped: true, want: ['api', 'worker', 'pg', 'old', 'new'] },
		{ name: 'stopped ones hidden', q: '', stopped: false, want: ['api', 'worker', 'pg'] },
		{ name: 'filter', q: 'wor', stopped: true, want: ['worker'] },
		{ name: 'filter and hidden', q: 'old', stopped: false, want: [] },
		{ name: 'filter by state', q: 'restarting', stopped: false, want: ['worker'] }
	];
	test.each(cases)('$name', ({ q, stopped, want }) => {
		expect(names(q, stopped)).toEqual(want);
	});
});

describe('cpuShare, memShare and cpuMeter', () => {
	test('CPU is a share of the CPUs the container sees', () => {
		expect(cpuShare({ cpu: 100, cpus: 4 })).toBe(25);
		expect(cpuShare({ cpu: 150, cpus: 0 })).toBe(150);
		expect(cpuShare({ cpu: null, cpus: 4 })).toBeNull();
		expect(cpuShare(null)).toBeNull();
	});

	test('memory is a share of its limit', () => {
		expect(memShare({ memUsed: 256, memLimit: 1024 })).toBe(25);
		expect(memShare({ memUsed: 256, memLimit: 0 })).toBeNull();
		expect(memShare({ memUsed: null, memLimit: 1024 })).toBeNull();
		expect(memShare(undefined)).toBeNull();
	});

	test('the total CPU is a share of the machine', () => {
		expect(cpuMeter(200, 4)).toBe(50);
		expect(cpuMeter(200, 0)).toBeNull();
		expect(cpuMeter(200, null)).toBeNull();
	});
});

describe('uptimeLabel', () => {
	const now = Date.parse('2027-01-01T12:00:00Z');

	test('running with a start time', () => {
		expect(uptimeLabel(row({ startedAt: '2027-01-01T09:00:00Z' }), now)).toStartWith('up since ');
	});

	test('stopped says how long ago', () => {
		const c = row({ state: 'exited', status: 'Exited (0)', finishedAt: '2027-01-01T09:00:00Z' });
		expect(uptimeLabel(c, now)).toBe('stopped 3h ago');
	});

	test.each([
		{ name: 'running without a start', over: { startedAt: undefined } },
		{ name: 'a start Docker never set', over: { startedAt: '0001-01-01T00:00:00Z' } },
		{ name: 'restarting', over: { state: 'restarting', startedAt: '2027-01-01T09:00:00Z' } },
		{ name: 'never ran', over: { state: 'created', finishedAt: '0001-01-01T00:00:00Z' } }
	])('$name says what Docker says', ({ over }) => {
		expect(uptimeLabel(row({ status: 'what Docker says', ...over }), now)).toBe('what Docker says');
	});
});

describe('portChips', () => {
	test('shows the published ports first', () => {
		const { shown, more } = portChips([
			{ private: 80, public: 8080, type: 'tcp', ip: '10.0.0.5' },
			{ private: 443, type: 'tcp' },
			{ private: 53, public: 5353, type: 'udp' }
		]);
		expect(shown).toEqual([
			{ label: '8080→80', title: '10.0.0.5:8080→80', published: true },
			{ label: '5353→53/udp', title: '5353→53/udp', published: true }
		]);
		expect(more).toBe(0);
	});

	test('falls back to the ports nobody published', () => {
		const { shown } = portChips([{ private: 5432, type: 'tcp' }]);
		expect(shown).toEqual([{ label: '5432', title: '5432', published: false }]);
	});

	test('leaves out repeats, and counts what does not fit', () => {
		const ports = [80, 81, 82, 83].flatMap((p) => [
			{ private: p, public: p, type: 'tcp' },
			{ private: p, public: p, type: 'tcp', ip: '::1' }
		]);
		const { shown, more } = portChips(ports, 3);
		expect(shown.map((c) => c.label)).toEqual(['80→80', '81→81', '82→82']);
		expect(more).toBe(1);
	});

	test('no ports', () => {
		expect(portChips([])).toEqual({ shown: [], more: 0 });
	});
});

describe('reclaimable', () => {
	test('adds images and volumes', () => {
		const group = (reclaimable: number) => ({ size: 0, count: 0, active: 0, reclaimable });
		const disk: DockerDisk = {
			images: group(100),
			containers: group(7),
			volumes: group(20),
			buildCache: group(5),
			total: 0,
			at: ''
		};
		expect(reclaimable(disk)).toBe(120);
	});
});

describe('pacedOverview', () => {
	function overview(disk: boolean): DockerOverview {
		return { disk: disk ? ({} as DockerDisk) : null } as DockerOverview;
	}

	test('asks every time until the disk is known, then every 30 seconds', async () => {
		let clock = 0;
		const answers = [overview(false), overview(false), overview(true), overview(true)];
		let asked = 0;
		const load = pacedOverview(
			async () => answers[asked++]!,
			30_000,
			() => clock
		);

		await load();
		clock += 4000;
		await load();
		clock += 4000;
		const measured = await load();
		expect(asked).toBe(3);
		expect(measured.disk).not.toBeNull();

		clock += 4000;
		expect(await load()).toBe(measured);
		clock += 20_000;
		expect(await load()).toBe(measured);
		expect(asked).toBe(3);

		clock += 10_000;
		await load();
		expect(asked).toBe(4);
	});

	test('a failed ask fails the poll and is asked again', async () => {
		let fail = true;
		let asked = 0;
		const load = pacedOverview(async () => {
			asked++;
			if (fail) throw new Error('no socket');
			return overview(true);
		});
		await expect(load()).rejects.toThrow('no socket');
		fail = false;
		await load();
		expect(asked).toBe(2);
	});
});

describe('volumes, images and networks', () => {
	test('volumeName cuts anonymous volumes', () => {
		const anonymous = '0123456789abcdef'.repeat(4);
		expect(volumeName(anonymous)).toBe('0123456789ab…');
		expect(volumeName('pgdata')).toBe('pgdata');
		expect(volumeName('0123456789abcdef')).toBe('0123456789abcdef');
	});

	test('volumeTotals counts the sizes that are known', () => {
		const volume = (over: Partial<DockerVolume>) => over as DockerVolume;
		expect(
			volumeTotals([
				volume({ size: 100, usedBy: [] }),
				volume({ usedBy: [{ id: 'a', name: 'db', running: true }] }),
				volume({ size: 50, usedBy: [] })
			])
		).toEqual({ count: 3, size: 150, measured: true, unused: 2 });
		expect(volumeTotals([volume({ usedBy: [] })])).toEqual({
			count: 1,
			size: 0,
			measured: false,
			unused: 1
		});
		expect(volumeTotals([])).toEqual({ count: 0, size: 0, measured: false, unused: 0 });
	});

	test('imageTotals', () => {
		const image = (over: Partial<DockerImage>) => over as DockerImage;
		expect(
			imageTotals([
				image({ size: 100, usedBy: [{ id: 'a', name: 'db', running: false }], dangling: false }),
				image({ size: 40, usedBy: [], dangling: true }),
				image({ size: 10, usedBy: [], dangling: false })
			])
		).toEqual({ count: 3, size: 150, unused: 2, dangling: 1 });
	});

	test('shortDigest', () => {
		const hash = 'sha256:' + 'ab12'.repeat(16);
		expect(shortDigest(`nginx@${hash}`)).toBe('sha256:ab12ab12ab12');
		expect(shortDigest(hash)).toBe('sha256:ab12ab12ab12');
		expect(shortDigest('nginx@abcdef0123456789')).toBe('abcdef012345');
		expect(shortDigest(undefined)).toBeNull();
	});

	test('networkTotals', () => {
		const net = (n: number) => ({ containers: new Array(n).fill({}) }) as DockerNetwork;
		expect(networkTotals([net(2), net(0), net(1)])).toEqual({ count: 3, inUse: 2 });
	});
});
