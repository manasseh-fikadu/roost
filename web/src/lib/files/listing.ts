// The listing's layout and the arithmetic of drawing only the rows on screen.

/** The columns, narrowing with the screen: name only, then size and time, then all of them. */
export const gridClass =
	'grid grid-cols-[32px_minmax(0,1fr)_36px] sm:grid-cols-[36px_minmax(0,1fr)_84px_116px_36px] lg:grid-cols-[36px_minmax(0,1fr)_88px_120px_104px_112px_40px]';

/** Rows have a fixed height, so the ones on screen are the only ones that need to exist. */
export const ROW_MOBILE = 40;
export const ROW_DESKTOP = 36;
export const OVERSCAN = 15;
/** The room at the top for the app's header and the listing's sticky column header. */
export const STICKY_TOP = 56 + 36;
/** The room at the bottom that the floating selection bar may cover. */
export const BOTTOM_GAP = 72;

/**
 * The rows to draw when the list's top is `top` pixels from the top of the window, and the
 * window is `viewport` pixels tall.
 */
export function visibleRange(
	top: number,
	viewport: number,
	rowH: number,
	count: number
): { start: number; end: number } {
	const first = Math.floor(-top / rowH);
	return {
		start: Math.max(0, first - OVERSCAN),
		end: Math.min(count, Math.max(0, first) + Math.ceil(viewport / rowH) + OVERSCAN)
	};
}

/**
 * How far to scroll the window to bring row `cursor` into view, or 0 if it is. `top` is the
 * list's distance from the top of the window.
 */
export function scrollToRow(top: number, cursor: number, rowH: number, viewport: number): number {
	const rowTop = top + cursor * rowH;
	if (rowTop < STICKY_TOP) {
		return rowTop - STICKY_TOP - 4;
	}
	if (rowTop + rowH > viewport - BOTTOM_GAP) {
		return rowTop + rowH - viewport + BOTTOM_GAP;
	}
	return 0;
}
