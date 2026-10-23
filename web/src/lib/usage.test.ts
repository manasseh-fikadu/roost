import { describe, expect, test } from 'bun:test';
import type { UsageHistory, UsageSeries } from './api/types.js';
import { bytesAxisMax, cpuText, groupName, otherColor, seriesColors, usageValue } from './usage.js';

describe('cpuText', () => {
	test.each([
		{ name: 'missing', v: null, want: '—' },
		{ name: 'not a number', v: Number.NaN, want: '—' },
		{ name: 'none', v: 0, want: '0%' },
		{ name: 'a sliver', v: 0.04, want: '<0.1%' },
		{ name: 'under ten has a decimal', v: 4.24, want: '4.2%' },
		{ name: 'ten and over has none', v: 37.4, want: '37%' }
	])('$name', ({ v, want }) => {
		expect(cpuText(v)).toBe(want);
	});
});

describe('usageValue', () => {
	test.each([
		{ name: 'CPU', metric: 'cpu' as const, v: 12.5, want: '13%' },
		{ name: 'memory', metric: 'mem' as const, v: 1536, want: '1.5 KB' }
	])('$name', ({ metric, v, want }) => {
		expect(usageValue(metric, v)).toBe(want);
	});
});

describe('bytesAxisMax', () => {
	const gb = 1024 ** 3;
	test.each([
		{ name: 'nothing', values: [], want: gb },
		{ name: 'only gaps', values: [null, 0], want: gb },
		{ name: 'rounds up to a power of two', values: [2.5 * gb], want: 4 * gb },
		{ name: 'leaves room above the top', values: [null, 3.9 * gb], want: 8 * gb },
		{ name: 'megabytes', values: [300 * 1024 ** 2], want: 512 * 1024 ** 2 }
	])('$name', ({ values, want }) => {
		expect(bytesAxisMax(values)).toBe(want);
	});
});

describe('seriesColors', () => {
	const series = (...keys: string[]): UsageSeries[] =>
		keys.map((key) => ({ key, kind: 'service', name: key, points: [] }));
	const history = (cpu: UsageSeries[], mem: UsageSeries[]): UsageHistory => ({
		from: '',
		to: '',
		step: 60_000,
		minutes: 1,
		consumers: [],
		cpu,
		mem
	});

	test('a group has one color for CPU and memory', () => {
		const colors = seriesColors(history(series('a', 'b'), series('b', 'c')));
		expect([...colors.keys()]).toEqual(['a', 'b', 'c']);
		expect(new Set(colors.values()).size).toBe(3);
	});

	test('groups past the palette are gray', () => {
		const keys = Array.from({ length: 10 }, (_, i) => `g${i}`);
		const colors = seriesColors(history(series(...keys), []));
		expect(colors.get('g9')).toBe(otherColor);
		expect(colors.get('g0')).not.toBe(otherColor);
	});
});

describe('groupName', () => {
	test.each([
		{ key: 'container:web', want: 'web' },
		{ key: 'user-service:a:b', want: 'a:b' },
		{ key: 'plain', want: 'plain' }
	])('$key', ({ key, want }) => {
		expect(groupName(key)).toBe(want);
	});
});
