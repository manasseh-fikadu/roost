import { describe, expect, test } from 'bun:test';
import {
	ago,
	bytes,
	duration,
	latency,
	osLabel,
	percent,
	rate,
	rateShort,
	ratio
} from './format.js';

describe('bytes', () => {
	test.each([
		{ name: 'missing', n: null, want: '—' },
		{ name: 'not a number', n: Number.NaN, want: '—' },
		{ name: 'none', n: 0, want: '0 B' },
		{ name: 'bytes have no fraction', n: 512, want: '512 B' },
		{ name: 'kilobytes with a fraction', n: 1536, want: '1.5 KB' },
		{ name: 'three digits need no fraction', n: 300 * 1024, want: '300 KB' },
		{ name: 'gigabytes', n: 16.7 * 1024 ** 3, want: '16.7 GB' },
		{ name: 'stops at petabytes', n: 5 * 1024 ** 5, want: '5.0 PB' }
	])('$name', ({ n, want }) => {
		expect(bytes(n)).toBe(want);
	});
});

describe('rate and rateShort', () => {
	test.each([
		{ name: 'missing', n: null, long: '—', short: '—' },
		{ name: 'bytes', n: 402, long: '402 B/s', short: '402B' },
		{ name: 'kilobytes', n: 9.1 * 1024, long: '9.1 KB/s', short: '9.1K' },
		{ name: 'three digits', n: 150 * 1024 * 1024, long: '150 MB/s', short: '150M' }
	])('$name', ({ n, long, short }) => {
		expect(rate(n)).toBe(long);
		expect(rateShort(n)).toBe(short);
	});
});

describe('percent of a ratio', () => {
	test.each([
		{ name: 'most', used: 95, total: 100, want: '95%' },
		{ name: 'rounds', used: 1, total: 3, want: '33%' },
		{ name: 'no total', used: 5, total: 0, want: '—' },
		{ name: 'nothing used is known', used: null, total: 100, want: '—' },
		{ name: 'no total is known', used: 5, total: undefined, want: '—' }
	])('$name', ({ used, total, want }) => {
		expect(percent(ratio(used, total))).toBe(want);
	});

	test('takes digits', () => {
		expect(percent(34.56, 1)).toBe('34.6%');
	});
});

describe('latency', () => {
	test.each([
		{ name: 'missing', ms: undefined, want: '—' },
		{ name: 'under a millisecond', ms: 0.42, want: '0.42 ms' },
		{ name: 'under ten', ms: 3.84, want: '3.8 ms' },
		{ name: 'tens', ms: 80.4, want: '80 ms' },
		{ name: 'seconds stay in milliseconds', ms: 1234, want: '1234 ms' }
	])('$name', ({ ms, want }) => {
		expect(latency(ms)).toBe(want);
	});
});

describe('duration', () => {
	test.each([
		{ name: 'missing', s: null, want: '—' },
		{ name: 'never below zero', s: -5, want: '0s' },
		{ name: 'seconds', s: 40, want: '40s' },
		{ name: 'whole minutes', s: 300, want: '5m' },
		{ name: 'hours and minutes', s: 7500, want: '2h 5m' },
		{ name: 'days and hours', s: 3 * 86_400 + 10 * 3600 + 59, want: '3d 10h' }
	])('$name', ({ s, want }) => {
		expect(duration(s)).toBe(want);
	});
});

describe('ago', () => {
	const now = Date.UTC(2026, 9, 22, 12, 0, 0);
	test.each([
		{ name: 'never', t: null, want: 'never' },
		{ name: 'not a time', t: 'yesterday', want: 'never' },
		{ name: 'a moment ago', t: now - 2_000, want: 'just now' },
		{ name: 'seconds', t: now - 30_000, want: '30s ago' },
		{ name: 'minutes', t: now - 5 * 60_000, want: '5m ago' },
		{ name: 'hours', t: now - 3 * 3_600_000, want: '3h ago' },
		{
			name: 'days, from RFC 3339',
			t: new Date(now - 4 * 86_400_000).toISOString(),
			want: '4d ago'
		},
		{ name: 'in the future', t: now + 2 * 86_400_000 + 3 * 3_600_000, want: 'in 2d 3h' }
	])('$name', ({ t, want }) => {
		expect(ago(t, now)).toBe(want);
	});
});

describe('osLabel', () => {
	test.each([
		{ name: 'Linux', os: 'linux', want: 'Linux' },
		{ name: 'macOS as it is', os: 'macOS', want: 'macOS' },
		{ name: 'iOS', os: 'iOS', want: 'iOS' },
		{ name: 'Android', os: 'android', want: 'Android' },
		{ name: 'one Roost doesn’t know', os: 'plan9', want: 'plan9' },
		{ name: 'none', os: '', want: 'Unknown OS' }
	])('$name', ({ os, want }) => {
		expect(osLabel(os)).toBe(want);
	});
});
