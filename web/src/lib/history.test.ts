import { expect, test } from 'bun:test';
import { stepValues } from './history.js';

test('stepValues fills the steps that have no point', () => {
	const from = Date.parse('2026-10-23T12:00:00Z');
	const span = {
		from: new Date(from).toISOString(),
		to: new Date(from + 4 * 60_000 + 30_000).toISOString(),
		step: 60_000
	};
	const points = [
		{ ts: from, v: 100 },
		{ ts: from + 2 * 60_000, v: 75 },
		{ ts: from + 4 * 60_000, v: 50 }
	];
	expect(stepValues(span, points)).toEqual([100, null, 75, null, 50]);
});
