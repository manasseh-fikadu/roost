import { describe, expect, test } from 'bun:test';
import type { Check, CheckResult, CheckState } from './api/types.js';
import {
	addLatest,
	every,
	groupChecks,
	inputOf,
	matches,
	parseFilter,
	recentCount,
	type CheckFilter
} from './checks.js';

function check(id: string, group: string, state: CheckState = 'passing'): Check {
	return {
		id,
		name: id,
		kind: 'http',
		target: `https://${id}.example.com/health?item=1`,
		endpoint: `https://${id}.example.com`,
		group,
		intervalSec: 60,
		timeoutMs: 5000,
		state,
		stateReason: '',
		failures: 0,
		uptime: null
	};
}

describe('parseFilter', () => {
	test.each<{ query: string | null; want: CheckFilter }>([
		{ query: null, want: 'all' },
		{ query: 'failing', want: 'failing' },
		{ query: 'unavailable', want: 'unavailable' },
		{ query: 'broken', want: 'all' }
	])('?state=$query', ({ query, want }) => {
		expect(parseFilter(query)).toBe(want);
	});
});

describe('matches', () => {
	test.each<{ state: CheckState; filter: CheckFilter; want: boolean }>([
		{ state: 'passing', filter: 'all', want: true },
		{ state: 'passing', filter: 'passing', want: true },
		{ state: 'failing', filter: 'passing', want: false },
		{ state: 'failing', filter: 'failing', want: true },
		{ state: 'pending', filter: 'unavailable', want: true },
		{ state: 'paused', filter: 'unavailable', want: true },
		{ state: 'unavailable', filter: 'unavailable', want: true },
		{ state: 'failing', filter: 'unavailable', want: false }
	])('a $state check with the $filter filter', ({ state, filter, want }) => {
		expect(matches(check('api', 'Apps', state), filter)).toBe(want);
	});
});

test('groupChecks keeps the order the checks come in', () => {
	const groups = groupChecks([
		check('api', 'Apps'),
		check('dns', 'Internet'),
		check('web', 'Apps'),
		check('ping', 'Tailnet')
	]);
	expect(groups.map((g) => [g.name, g.checks.map((c) => c.id)])).toEqual([
		['Apps', ['api', 'web']],
		['Internet', ['dns']],
		['Tailnet', ['ping']]
	]);
});

test('inputOf keeps a check as it is, with changes', () => {
	const c = { ...check('api', 'Apps'), expectStatus: 204, insecure: true };
	expect(inputOf(c, { paused: true })).toEqual({
		name: 'api',
		kind: 'http',
		target: 'https://api.example.com/health?item=1',
		group: 'Apps',
		intervalSec: 60,
		timeoutMs: 5000,
		expectStatus: 204,
		insecure: true,
		paused: true
	});
});

describe('every', () => {
	test.each([
		{ seconds: 30, want: 'every 30s' },
		{ seconds: 90, want: 'every 90s' },
		{ seconds: 300, want: 'every 5m' },
		{ seconds: 7200, want: 'every 2h' }
	])('$seconds seconds', ({ seconds, want }) => {
		expect(every(seconds)).toBe(want);
	});
});

describe('addLatest', () => {
	const start = Date.parse('2026-10-23T12:00:00Z');
	const result = (minute: number, ok = true): CheckResult => ({
		at: new Date(start + minute * 60_000).toISOString(),
		ok,
		detail: `minute ${minute}`
	});
	const withLast = (id: string, last?: CheckResult): Check => ({ ...check(id, 'Apps'), last });

	test('adds a newer result', () => {
		const byId = { api: [result(0), result(1)] };
		const got = addLatest(byId, [withLast('api', result(2, false))]);
		expect(got.api).toEqual([result(0), result(1), result(2, false)]);
		expect(byId.api).toHaveLength(2);
	});

	test('keeps the same object when nothing is new', () => {
		const byId = { api: [result(0), result(1)] };
		expect(addLatest(byId, [withLast('api', result(1)), withLast('db')])).toBe(byId);
	});

	test("starts a check's results", () => {
		expect(addLatest({}, [withLast('db', result(5))])).toEqual({ db: [result(5)] });
	});

	test(`keeps the latest ${recentCount}`, () => {
		const full = Array.from({ length: recentCount }, (_, i) => result(i));
		const got = addLatest({ api: full }, [withLast('api', result(recentCount))]);
		expect(got.api).toHaveLength(recentCount);
		expect(got.api[0]).toEqual(result(1));
		expect(got.api.at(-1)).toEqual(result(recentCount));
	});
});
