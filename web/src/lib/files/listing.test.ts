import { expect, test } from 'bun:test';
import { BOTTOM_GAP, OVERSCAN, STICKY_TOP, scrollToRow, visibleRange } from './listing.js';

test('the rows to draw follow the scroll', () => {
	// At the top, the rows on screen and some below.
	expect(visibleRange(100, 900, 36, 1000)).toEqual({ start: 0, end: 25 + OVERSCAN });
	// Scrolled 3600px, so row 100 is at the top.
	expect(visibleRange(-3600, 900, 36, 1000)).toEqual({
		start: 100 - OVERSCAN,
		end: 100 + 25 + OVERSCAN
	});
	// Past the end, and with few rows.
	expect(visibleRange(-100000, 900, 36, 1000).end).toBe(1000);
	expect(visibleRange(0, 900, 36, 3)).toEqual({ start: 0, end: 3 });
});

test('the cursor is scrolled into view', () => {
	// Visible: no scroll.
	expect(scrollToRow(200, 3, 36, 900)).toBe(0);
	// Above the sticky header.
	expect(scrollToRow(-500, 2, 36, 900)).toBeLessThan(0);
	expect(scrollToRow(-500, 2, 36, 900)).toBe(-500 + 72 - STICKY_TOP - 4);
	// Below the fold, with room for the selection bar.
	expect(scrollToRow(0, 30, 36, 900)).toBe(30 * 36 + 36 - 900 + BOTTOM_GAP);
});
