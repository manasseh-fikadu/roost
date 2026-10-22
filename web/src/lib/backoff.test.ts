import { describe, expect, test } from 'bun:test';
import { backoff } from './backoff.js';

describe('backoff', () => {
	test.each([
		{ name: 'first failure, least wait', attempt: 0, random: 0, want: 500 },
		{ name: 'first failure, most wait', attempt: 0, random: 1, want: 1_000 },
		{ name: 'doubles', attempt: 1, random: 1, want: 2_000 },
		{ name: 'doubles again', attempt: 3, random: 0.5, want: 6_000 },
		{ name: 'stops at 30 seconds', attempt: 5, random: 1, want: 30_000 },
		{ name: 'stays at 30 seconds', attempt: 50, random: 0, want: 15_000 }
	])('$name', ({ attempt, random, want }) => {
		expect(backoff(attempt, () => random)).toBe(want);
	});
});
