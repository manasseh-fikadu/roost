import { describe, expect, test } from 'bun:test';
import type { DockerContainerRow } from '#lib/api/types.js';
import type { Tone } from '#lib/status.js';
import {
	actionsFor,
	argv,
	groupRows,
	matchesFilter,
	portLabel,
	rowStatus,
	shortId,
	stateBadge,
	totals
} from './docker.js';

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

describe('rowStatus', () => {
	const cases: [string, Partial<DockerContainerRow>, string][] = [
		['running', { state: 'running' }, 'online'],
		['healthy', { state: 'running', health: 'healthy' }, 'online'],
		['still starting', { state: 'running', health: 'starting' }, 'online'],
		['unhealthy', { state: 'running', health: 'unhealthy' }, 'attention'],
		['restarting', { state: 'restarting' }, 'attention'],
		['paused', { state: 'paused' }, 'attention'],
		['dead', { state: 'dead' }, 'offline'],
		['stopped cleanly', { state: 'exited', exitCode: 0 }, 'unknown'],
		['stopped by docker stop', { state: 'exited', exitCode: 137 }, 'unknown'],
		['crashed', { state: 'exited', exitCode: 1 }, 'offline'],
		['out of memory', { state: 'exited', exitCode: 0, oomKilled: true }, 'offline'],
		['exited without a code', { state: 'exited' }, 'unknown'],
		['created', { state: 'created' }, 'unknown']
	];
	test.each(cases)('%s', (_, over, want) => {
		expect(rowStatus(row(over))).toBe(want as ReturnType<typeof rowStatus>);
	});
});

describe('stateBadge', () => {
	const cases: [string, Partial<DockerContainerRow>, string, Tone][] = [
		['running', { state: 'running' }, 'running', 'green'],
		['healthy', { state: 'running', health: 'healthy' }, 'running', 'green'],
		['unhealthy', { state: 'running', health: 'unhealthy' }, 'unhealthy', 'amber'],
		['starting', { state: 'running', health: 'starting' }, 'starting', 'blue'],
		['restarting', { state: 'restarting' }, 'restarting', 'amber'],
		['paused', { state: 'paused' }, 'paused', 'blue'],
		['crashed', { state: 'exited', exitCode: 3 }, 'exited (3)', 'red'],
		[
			'killed for memory',
			{ state: 'exited', exitCode: 137, oomKilled: true },
			'out of memory',
			'red'
		],
		['stopped', { state: 'exited', exitCode: 137 }, 'stopped', 'gray'],
		['created', { state: 'created' }, 'created', 'gray'],
		['dead', { state: 'dead' }, 'dead', 'red']
	];
	test.each(cases)('%s', (_, over, label, tone) => {
		expect(stateBadge(row(over))).toEqual({ label, tone });
	});
});

test('actionsFor', () => {
	expect(actionsFor('running')).toEqual(['restart', 'stop']);
	expect(actionsFor('restarting')).toEqual(['restart', 'stop']);
	expect(actionsFor('paused')).toEqual(['stop']);
	expect(actionsFor('exited')).toEqual(['start']);
	expect(actionsFor('created')).toEqual(['start']);
});

test('portLabel', () => {
	expect(portLabel({ private: 80, public: 8080, type: 'tcp' })).toBe('8080→80');
	expect(portLabel({ private: 53, public: 53, type: 'udp' })).toBe('53→53/udp');
	expect(portLabel({ private: 443, type: 'tcp' })).toBe('443');
	expect(portLabel({ private: 443, type: 'sctp' })).toBe('443/sctp');
});

test('argv quotes what needs it', () => {
	expect(argv(['nginx', '-g', 'daemon off;'])).toBe('nginx -g "daemon off;"');
	expect(argv(['/bin/sh', '-c', 'echo "hi"'])).toBe('/bin/sh -c "echo \\"hi\\""');
	expect(argv(['--user=a:b', '--p=1,2'])).toBe('--user=a:b --p=1,2');
	expect(argv(null)).toBe('');
	expect(argv([])).toBe('');
});

test('shortId', () => {
	expect(shortId('a'.repeat(64))).toBe('a'.repeat(12));
	expect(shortId('sha256:' + 'b'.repeat(64))).toBe('b'.repeat(12));
	expect(shortId('abc')).toBe('abc');
});

test('groupRows groups by project, by name, and then the loose ones', () => {
	const rows = [
		row({ name: 'z' }),
		row({ name: 'b-web', project: 'shop' }),
		row({ name: 'a-db', project: 'blog' }),
		row({ name: 'b-db', project: 'shop' })
	];
	expect(groupRows(rows).map((g) => [g.project, g.containers.map((c) => c.name)])).toEqual([
		['blog', ['a-db']],
		['shop', ['b-web', 'b-db']],
		[null, ['z']]
	]);
	expect(groupRows([])).toEqual([]);
});

describe('matchesFilter', () => {
	const c = row({
		name: 'shop-web-1',
		image: 'nginx:1.27',
		project: 'shop',
		service: 'web',
		health: 'healthy',
		ports: [{ private: 80, public: 8080, type: 'tcp' }],
		networks: ['shop_default']
	});
	test.each([
		['', true],
		['  ', true],
		['nginx', true],
		['NGINX shop', true],
		['8080', true],
		['shop_default', true],
		['healthy', true],
		['running web', true],
		['postgres', false],
		['nginx postgres', false]
	])('%j', (query, want) => {
		expect(matchesFilter(c, query)).toBe(want);
	});
});

test('totals adds up the running containers', () => {
	const stats = (cpu: number, mem: number) => ({
		ts: 1,
		cpu,
		memUsed: mem,
		memLimit: 100,
		netRx: 10,
		netTx: 5,
		netRxTotal: null,
		netTxTotal: null,
		blkRead: null,
		blkWrite: null,
		blkReadRate: null,
		blkWriteRate: null,
		pids: null,
		cpus: 4
	});
	const got = totals([
		row({ name: 'a', stats: stats(10, 100) }),
		row({ name: 'b', stats: stats(20, 200), health: 'unhealthy' }),
		row({ name: 'c', state: 'exited', health: 'unhealthy', stats: null }),
		row({ name: 'd', stats: null }),
		row({ name: 'e', state: 'restarting' })
	]);
	expect(got).toEqual({
		total: 5,
		running: 3,
		notRunning: 2,
		unhealthy: 1,
		cpu: 30,
		mem: 300,
		rx: 20,
		tx: 10,
		cpus: 4
	});
});
