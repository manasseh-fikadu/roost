import { describe, expect, test } from 'bun:test';
import type { DockerStats } from '#lib/api/types.js';
import {
	binaryAxisMax,
	bytesLabel,
	cpuAxisMax,
	hasData,
	memoryAxisMax,
	rateAxisMax,
	rateLabel,
	samplePoints,
	stepPoints
} from './charts.js';

const KB = 1024;
const MB = KB * 1024;
const GB = MB * 1024;

function stats(over: Partial<DockerStats> = {}): DockerStats {
	return {
		ts: 0,
		cpu: 0,
		memUsed: 0,
		memLimit: null,
		netRx: 0,
		netTx: 0,
		netRxTotal: 0,
		netTxTotal: 0,
		blkRead: 0,
		blkWrite: 0,
		blkReadRate: 0,
		blkWriteRate: 0,
		pids: 1,
		cpus: 1,
		...over
	};
}

describe('samplePoints', () => {
	test('one point per sample, at its time', () => {
		const out = samplePoints(
			[stats({ ts: 1000, cpu: 5 }), stats({ ts: 2000, cpu: 7.5 })],
			(s) => s.cpu
		);
		expect(out).toEqual([
			{ ts: 1000, v: 5 },
			{ ts: 2000, v: 7.5 }
		]);
	});

	test('a missing value is a gap', () => {
		const out = samplePoints([stats({ ts: 1, cpu: null }), stats({ ts: 2, cpu: 0 })], (s) => s.cpu);
		expect(out).toEqual([
			{ ts: 1, v: null },
			{ ts: 2, v: 0 }
		]);
	});

	test('a value that is not a number is a gap', () => {
		const out = samplePoints([stats({ cpu: NaN }), stats({ cpu: Infinity })], (s) => s.cpu);
		expect(out.map((p) => p.v)).toEqual([null, null]);
	});

	test('no samples, no points', () => {
		expect(samplePoints([], (s) => s.cpu)).toEqual([]);
	});
});

describe('stepPoints', () => {
	const span = { from: '2026-10-30T12:00:00Z', to: '2026-10-30T12:05:00Z', step: 60_000 };
	const t0 = Date.parse(span.from);

	test('has a point at every step, with a gap where the hub had none', () => {
		const out = stepPoints(span, [
			{ ts: t0, v: 1 },
			{ ts: t0 + 60_000, v: 2 },
			{ ts: t0 + 240_000, v: 5 }
		]);
		expect(out).toEqual([
			{ ts: t0, v: 1 },
			{ ts: t0 + 60_000, v: 2 },
			{ ts: t0 + 120_000, v: null },
			{ ts: t0 + 180_000, v: null },
			{ ts: t0 + 240_000, v: 5 },
			{ ts: t0 + 300_000, v: null }
		]);
	});

	test('keeps a null the hub sent', () => {
		const out = stepPoints(span, [{ ts: t0, v: null }]);
		expect(out[0]).toEqual({ ts: t0, v: null });
	});

	test('a history with nothing is all gaps', () => {
		expect(stepPoints(span, []).every((p) => p.v === null)).toBe(true);
		expect(stepPoints(span, []).length).toBe(6);
	});

	test('leaves the points alone if the step makes no sense', () => {
		const points = [{ ts: t0, v: 1 }];
		expect(stepPoints({ ...span, step: 0 }, points)).toBe(points);
		expect(stepPoints({ ...span, step: -5 }, points)).toBe(points);
		expect(stepPoints({ ...span, from: 'soon' }, points)).toBe(points);
	});

	test('leaves the points alone if there would be too many steps', () => {
		const points = [{ ts: t0, v: 1 }];
		expect(stepPoints({ ...span, step: 1 }, points)).toBe(points);
	});
});

describe('hasData', () => {
	test('is true if any point has a value, even zero', () => {
		expect(hasData([{ ts: 1, v: null }], [{ ts: 1, v: 0 }])).toBe(true);
	});

	test('is false with only gaps or nothing', () => {
		expect(hasData([{ ts: 1, v: null }], [])).toBe(false);
		expect(hasData()).toBe(false);
	});
});

describe('cpuAxisMax', () => {
	const cases: { name: string; values: (number | null)[]; cpus: number; want: number }[] = [
		{ name: 'an idle container gets one CPU', values: [0.5, 1, 3], cpus: 4, want: 100 },
		{ name: 'no values', values: [], cpus: 4, want: 100 },
		{ name: 'only gaps', values: [null, null], cpus: 4, want: 100 },
		{ name: 'nearly one CPU stays at one', values: [85], cpus: 4, want: 100 },
		{ name: 'more than one CPU gets a round number above', values: [130], cpus: 4, want: 200 },
		{ name: 'a few CPUs', values: [250], cpus: 8, want: 500 },
		{ name: 'cannot be more than all the CPUs', values: [380], cpus: 4, want: 400 },
		{
			name: 'cannot be more than all the CPUs, even with a spike',
			values: [900],
			cpus: 2,
			want: 200
		},
		{ name: 'one CPU never goes over 100', values: [120], cpus: 1, want: 100 },
		{ name: 'an unknown number of CPUs has no cap', values: [900], cpus: 0, want: 1000 }
	];
	for (const c of cases) {
		test(c.name, () => {
			expect(cpuAxisMax(c.values, c.cpus)).toBe(c.want);
		});
	}
});

describe('binaryAxisMax', () => {
	const cases: { name: string; values: (number | null)[]; floor: number; want: number }[] = [
		{ name: 'nothing is the floor', values: [], floor: MB, want: MB },
		{ name: 'gaps are the floor', values: [null], floor: MB, want: MB },
		{ name: 'under the floor', values: [100], floor: MB, want: MB },
		{ name: 'a little over a megabyte', values: [1.2 * MB], floor: MB, want: 2 * MB },
		{ name: 'a power of two above', values: [300 * MB], floor: MB, want: 512 * MB },
		{ name: 'just under a gigabyte goes up to one', values: [900 * MB], floor: MB, want: GB },
		{ name: 'two gigabytes', values: [1.6 * GB], floor: MB, want: 2 * GB },
		{ name: 'room above the highest value', values: [512 * MB], floor: MB, want: GB },
		{ name: 'takes the highest of many', values: [1, 40 * MB, null, 3], floor: MB, want: 64 * MB }
	];
	for (const c of cases) {
		test(c.name, () => {
			expect(binaryAxisMax(c.values, c.floor)).toBe(c.want);
		});
	}
});

describe('rateAxisMax', () => {
	const cases: [string, (number | null)[], number][] = [
		['idle is 1 KB/s', [0, 0], KB],
		['a few bytes a second', [30, 80], KB],
		['300 KB/s', [300 * KB], 512 * KB],
		['3 MB/s', [3 * MB], 4 * MB],
		['gaps', [null], KB]
	];
	for (const [name, values, want] of cases) {
		test(name, () => {
			expect(rateAxisMax(values)).toBe(want);
		});
	}
});

describe('memoryAxisMax', () => {
	const cases: { name: string; values: (number | null)[]; limit: number | null; want: number }[] = [
		{
			name: 'the limit, when it is near what is used',
			values: [200 * MB],
			limit: 512 * MB,
			want: 512 * MB
		},
		{
			name: 'the limit, when little is used but not absurdly little',
			values: [30 * MB],
			limit: 512 * MB,
			want: 512 * MB
		},
		{
			name: 'not the limit, when it is far above what is used',
			values: [30 * MB],
			limit: 64 * GB,
			want: 64 * MB
		},
		{ name: 'no limit', values: [100 * MB], limit: null, want: 128 * MB },
		{ name: 'a limit of zero is no limit', values: [100 * MB], limit: 0, want: 128 * MB },
		{ name: 'no use at all and a small limit', values: [0], limit: 256 * MB, want: MB },
		{
			name: 'over the limit is still the limit',
			values: [600 * MB],
			limit: 512 * MB,
			want: 512 * MB
		}
	];
	for (const c of cases) {
		test(c.name, () => {
			expect(memoryAxisMax(c.values, c.limit)).toBe(c.want);
		});
	}
});

describe('labels', () => {
	test('bytes', () => {
		expect(bytesLabel(0)).toBe('0B');
		expect(bytesLabel(512 * MB)).toBe('512MB');
		expect(bytesLabel(2 * GB)).toBe('2GB');
	});

	test('rates', () => {
		expect(rateLabel(0)).toBe('0B/s');
		expect(rateLabel(KB)).toBe('1.0K/s');
		expect(rateLabel(512 * KB)).toBe('512K/s');
	});
});
