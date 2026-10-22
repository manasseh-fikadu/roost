import { describe, expect, test } from 'bun:test';
import { areaPath, linePath, nearest, niceMax, runs, typicalStep, type Point } from './path.js';

const identity = (n: number) => n;

describe('runs', () => {
	test.each<{ name: string; points: Point[]; maxGap: number; want: [number, number][][] }>([
		{ name: 'no points', points: [], maxGap: 1, want: [] },
		{
			name: 'one run',
			points: [
				{ ts: 0, v: 1 },
				{ ts: 1, v: 2 }
			],
			maxGap: 1,
			want: [
				[
					[0, 1],
					[1, 2]
				]
			]
		},
		{
			name: 'breaks at a null',
			points: [
				{ ts: 0, v: 1 },
				{ ts: 1, v: null },
				{ ts: 2, v: 3 }
			],
			maxGap: 1,
			want: [[[0, 1]], [[2, 3]]]
		},
		{
			name: 'breaks at a long gap',
			points: [
				{ ts: 0, v: 1 },
				{ ts: 1, v: 2 },
				{ ts: 5, v: 3 }
			],
			maxGap: 2,
			want: [
				[
					[0, 1],
					[1, 2]
				],
				[[5, 3]]
			]
		},
		{
			name: 'only nulls',
			points: [
				{ ts: 0, v: null },
				{ ts: 1, v: null }
			],
			maxGap: 1,
			want: []
		}
	])('$name', ({ points, maxGap, want }) => {
		expect(runs(points, identity, identity, maxGap)).toEqual(want);
	});
});

describe('paths', () => {
	const run: [number, number][] = [
		[0, 10],
		[5, 2.25]
	];

	test('linePath', () => {
		expect(linePath(run)).toBe('M0.0,10.0L5.0,2.3');
	});

	test('areaPath closes down to the baseline', () => {
		expect(areaPath(run, 20)).toBe('M0.0,10.0L5.0,2.3L5.0,20L0.0,20Z');
	});
});

describe('niceMax', () => {
	test.each([
		{ v: 0, want: 1 },
		{ v: -3, want: 1 },
		{ v: 1, want: 1 },
		{ v: 1.2, want: 2 },
		{ v: 2.2, want: 2.5 },
		{ v: 3, want: 5 },
		{ v: 7, want: 10 },
		{ v: 42, want: 50 },
		{ v: 1100, want: 2000 }
	])('$v → $want', ({ v, want }) => {
		expect(niceMax(v)).toBe(want);
	});
});

describe('nearest', () => {
	const sorted = [0, 10, 20, 30];
	test.each([
		{ target: -5, want: 0 },
		{ target: 4, want: 0 },
		{ target: 6, want: 1 },
		{ target: 20, want: 2 },
		{ target: 99, want: 3 }
	])('$target → index $want', ({ target, want }) => {
		expect(nearest(sorted, target)).toBe(want);
	});

	test('one value', () => {
		expect(nearest([7], 100)).toBe(0);
	});
});

describe('typicalStep', () => {
	test('is the median gap', () => {
		const points = [0, 60, 120, 900, 960].map((ts) => ({ ts, v: 1 }));
		expect(typicalStep(points)).toBe(60);
	});

	test('is a minute with too few points', () => {
		expect(typicalStep([{ ts: 0, v: 1 }])).toBe(60_000);
	});
});
