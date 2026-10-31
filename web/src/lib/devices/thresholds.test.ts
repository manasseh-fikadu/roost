import { describe, expect, test } from 'bun:test';
import { draftOf, emptyDraft, thresholdsOf, thresholdsProblem } from './thresholds.js';

describe('thresholds', () => {
	test('an empty form sends nothing', () => {
		expect(thresholdsOf(emptyDraft())).toEqual({});
		expect(thresholdsProblem(emptyDraft())).toBe('');
	});

	test('a device with some thresholds of its own comes back as it was', () => {
		const own = { diskWarn: 97, offlineMinutes: 60 };
		const draft = draftOf(own);
		expect(draft.memoryWarn).toBeNaN();
		expect(thresholdsOf(draft)).toEqual(own);
	});

	test('a device with none', () => {
		expect(thresholdsOf(draftOf(undefined))).toEqual({});
	});

	test.each([
		['a memory threshold over 100', { memoryWarn: 101 }, 'memory'],
		['a CPU threshold of 0', { cpuWarn: 0 }, 'CPU'],
		['a disk critical before its warning', { diskWarn: 90, diskCritical: 80 }, 'critical before'],
		['no time offline', { offlineMinutes: 0 }, 'offline'],
		['a fraction of a minute offline', { offlineMinutes: 1.5 }, 'offline']
	])('rejects %s', (_, change, words) => {
		expect(thresholdsProblem({ ...emptyDraft(), ...change })).toContain(words);
	});

	test('allows a warning alone, and the edges', () => {
		expect(thresholdsProblem({ ...emptyDraft(), diskWarn: 99 })).toBe('');
		expect(thresholdsProblem({ ...emptyDraft(), cpuWarn: 100, offlineMinutes: 10080 })).toBe('');
	});
});
