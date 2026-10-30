import { describe, expect, test } from 'bun:test';
import {
	click,
	emptySelection,
	move,
	toggle,
	toggleAll,
	toggleCursor,
	type Selection
} from './selection.js';

const rows = ['a', 'b', 'c', 'd', 'e'].map((path) => ({ path }));
const paths = (s: Selection) => [...s.selected].sort();

describe('click', () => {
	test('selects just the row', () => {
		const s = click(click(emptySelection(), rows, 1, {}), rows, 3, {});
		expect(paths(s)).toEqual(['d']);
		expect(s.anchor).toBe(3);
		expect(s.cursor).toBe(3);
	});
	test('shift extends from the anchor, either way', () => {
		let s = click(emptySelection(), rows, 1, {});
		s = click(s, rows, 3, { shift: true });
		expect(paths(s)).toEqual(['b', 'c', 'd']);
		s = click(s, rows, 0, { shift: true });
		expect(paths(s)).toEqual(['a', 'b']);
		expect(s.anchor).toBe(1);
	});
	test('meta toggles one row and keeps the others', () => {
		let s = click(emptySelection(), rows, 0, {});
		s = click(s, rows, 2, { meta: true });
		expect(paths(s)).toEqual(['a', 'c']);
		s = click(s, rows, 0, { meta: true });
		expect(paths(s)).toEqual(['c']);
	});
	test('meta and shift add a range to what is selected', () => {
		let s = click(emptySelection(), rows, 0, {});
		s = click(s, rows, 3, { meta: true });
		s = click(s, rows, 4, { meta: true, shift: true });
		expect(paths(s)).toEqual(['a', 'd', 'e']);
	});
	test('a row that is not there changes nothing', () => {
		const s = emptySelection();
		expect(click(s, rows, 9, {})).toBe(s);
	});
});

test('toggle adds to the selection', () => {
	let s = toggle(emptySelection(), rows, 1, false);
	s = toggle(s, rows, 3, false);
	expect(paths(s)).toEqual(['b', 'd']);
	s = toggle(s, rows, 1, false);
	expect(paths(s)).toEqual(['d']);
	s = toggle(s, rows, 0, true);
	expect(paths(s)).toEqual(['a', 'b', 'd']);
});

describe('move', () => {
	test('goes to a row and stays inside the list', () => {
		let s = move(emptySelection(), rows, 0, false);
		expect(s.cursor).toBe(0);
		s = move(s, rows, -3, false);
		expect(s.cursor).toBe(0);
		s = move(s, rows, 99, false);
		expect(s.cursor).toBe(4);
		expect(paths(s)).toEqual(['e']);
	});
	test('shift selects along the way, from where it started', () => {
		let s = move(emptySelection(), rows, 1, false);
		s = move(s, rows, 2, true);
		s = move(s, rows, 3, true);
		expect(paths(s)).toEqual(['b', 'c', 'd']);
		s = move(s, rows, 0, true);
		expect(paths(s)).toEqual(['a', 'b']);
	});
	test('shift from nowhere starts at the row it lands on', () => {
		const s = move(emptySelection(), rows, 2, true);
		expect(paths(s)).toEqual(['c']);
	});
	test('an empty list changes nothing', () => {
		const s = emptySelection();
		expect(move(s, [], 0, false)).toBe(s);
	});
});

test('space toggles the row under the cursor', () => {
	let s = move(emptySelection(), rows, 1, false);
	s = toggleCursor(s, rows);
	expect(paths(s)).toEqual([]);
	s = toggleCursor(s, rows);
	expect(paths(s)).toEqual(['b']);
});

test('select all, then none', () => {
	let s = toggleAll(emptySelection(), rows);
	expect(paths(s)).toEqual(['a', 'b', 'c', 'd', 'e']);
	s = toggleAll(s, rows);
	expect(paths(s)).toEqual([]);
	s = toggleAll(click(emptySelection(), rows, 0, {}), rows);
	expect(s.selected.size).toBe(5);
});
