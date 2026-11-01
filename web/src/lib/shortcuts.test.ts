import { describe, expect, test } from 'bun:test';
import { chordTimeout, isTyping, shortcutGroups, stepChord } from './shortcuts.js';

/** An element that is inside something that matches `selector`, or not. */
const inside = (matches: boolean) => ({
	closest: (selector: string) => (matches && selector.includes('textarea') ? {} : null)
});

describe('isTyping', () => {
	test.each([
		['a field', inside(true), true],
		['the page', inside(false), false],
		['nothing', null, false],
		['no target', undefined, false]
	])('%s', (_name, target, want) => {
		expect(isTyping(target)).toBe(want);
	});
});

describe('stepChord', () => {
	test('g arms the chord', () => {
		expect(stepChord(null, 'g', 100)).toEqual({ armedAt: 100 });
	});

	test.each(['1', '5', '9'])('g then %s goes to that page', (key) => {
		expect(stepChord(100, key, 200)).toEqual({ armedAt: null, page: Number(key) });
	});

	test('a number without g does nothing', () => {
		expect(stepChord(null, '2', 100)).toEqual({ armedAt: null });
	});

	test('0 is not a page', () => {
		expect(stepChord(100, '0', 200)).toEqual({ armedAt: null });
	});

	test('another key gives up', () => {
		expect(stepChord(100, 'x', 200)).toEqual({ armedAt: null });
	});

	test('g again starts over', () => {
		expect(stepChord(100, 'g', 300)).toEqual({ armedAt: 300 });
	});

	test('a number that comes too late does nothing', () => {
		expect(stepChord(100, '2', 100 + chordTimeout + 1)).toEqual({ armedAt: null });
		expect(stepChord(100, '2', 100 + chordTimeout)).toEqual({ armedAt: null, page: 2 });
	});
});

describe('shortcutGroups', () => {
	test('uses the computer’s name for the command key', () => {
		const keys = (mod: string) =>
			shortcutGroups(mod).flatMap((g) => g.shortcuts.flatMap((s) => s.keys));
		expect(keys('⌘')).toContain('⌘');
		expect(keys('⌘')).not.toContain('Ctrl+K');
		expect(keys('Ctrl')).not.toContain('⌘');
	});

	test('every shortcut has keys and a label', () => {
		for (const g of shortcutGroups('Ctrl')) {
			expect(g.shortcuts.length).toBeGreaterThan(0);
			for (const s of g.shortcuts) {
				expect(s.keys.length).toBeGreaterThan(0);
				expect(s.label).not.toBe('');
			}
		}
	});
});
