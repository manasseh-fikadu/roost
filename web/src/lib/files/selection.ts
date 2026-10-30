// Which rows of the listing are selected. These are pure functions, so the rules of clicking,
// shift-clicking and arrow keys are tested without a browser.

export interface Selection {
	selected: Set<string>;
	/** The row a shift-click or shift-arrow extends from. */
	anchor: number | null;
	/** The row the keyboard is on. */
	cursor: number;
}

export const emptySelection = (): Selection => ({ selected: new Set(), anchor: null, cursor: -1 });

interface Row {
	path: string;
}

interface Mods {
	shift?: boolean;
	/** Command on a Mac, Control elsewhere. */
	meta?: boolean;
}

function range(rows: Row[], a: number, b: number): string[] {
	const [lo, hi] = a < b ? [a, b] : [b, a];
	return rows.slice(lo, hi + 1).map((r) => r.path);
}

/** A click on a row: select just it, extend from the anchor with shift, or toggle it with meta. */
export function click(s: Selection, rows: Row[], index: number, mods: Mods): Selection {
	const row = rows[index];
	if (!row) {
		return s;
	}
	if (mods.shift && s.anchor !== null) {
		const next = new Set(mods.meta ? s.selected : []);
		for (const p of range(rows, s.anchor, index)) {
			next.add(p);
		}
		return { selected: next, anchor: s.anchor, cursor: index };
	}
	if (mods.meta) {
		const next = new Set(s.selected);
		if (!next.delete(row.path)) {
			next.add(row.path);
		}
		return { selected: next, anchor: index, cursor: index };
	}
	return { selected: new Set([row.path]), anchor: index, cursor: index };
}

/** A click on a row's checkbox, which adds to the selection and never replaces it. */
export function toggle(s: Selection, rows: Row[], index: number, shift: boolean): Selection {
	const row = rows[index];
	if (!row) {
		return s;
	}
	const next = new Set(s.selected);
	if (shift && s.anchor !== null) {
		for (const p of range(rows, s.anchor, index)) {
			next.add(p);
		}
	} else if (!next.delete(row.path)) {
		next.add(row.path);
	}
	return { selected: next, anchor: index, cursor: index };
}

/** An arrow key, Home, End or a page key: move the cursor to a row, selecting along with shift. */
export function move(s: Selection, rows: Row[], to: number, shift: boolean): Selection {
	const i = Math.max(0, Math.min(rows.length - 1, to));
	if (!rows[i]) {
		return s;
	}
	if (shift) {
		const anchor = s.anchor ?? (s.cursor >= 0 ? s.cursor : i);
		return { selected: new Set(range(rows, anchor, i)), anchor, cursor: i };
	}
	return { selected: new Set([rows[i].path]), anchor: i, cursor: i };
}

/** Space: toggles the row the keyboard is on, without moving. */
export function toggleCursor(s: Selection, rows: Row[]): Selection {
	const row = rows[s.cursor];
	if (!row) {
		return s;
	}
	const next = new Set(s.selected);
	if (!next.delete(row.path)) {
		next.add(row.path);
	}
	return { ...s, selected: next };
}

/** The checkbox in the header: everything, or nothing if everything was selected. */
export function toggleAll(s: Selection, rows: Row[]): Selection {
	const all = rows.length > 0 && rows.every((r) => s.selected.has(r.path));
	return { ...s, selected: all ? new Set() : new Set(rows.map((r) => r.path)) };
}
