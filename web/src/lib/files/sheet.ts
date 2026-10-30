// What the spreadsheet preview shows: a workbook read with SheetJS, cut down to a grid the page can
// render. It's apart from SheetViewer.svelte so bun test can load it. Import it only from the
// viewer, which the file manager loads on demand, so SheetJS stays out of the main bundle.

import {
	read,
	utils,
	type CellObject,
	type ColInfo,
	type ParsingOptions,
	type WorkBook,
	type WorkSheet
} from 'xlsx';
import { extOf } from './kinds.js';

export const MAX_ROWS = 5000;
export const MAX_COLS = 200;
/** The page gets sluggish past this many cells, so wide sheets show fewer rows. */
export const MAX_CELLS = 80_000;
/** The fewest rows a wide sheet is cut to. */
const MIN_ROWS = 200;
/** Column widths come from the first rows only. */
const WIDTH_SAMPLE = 300;

export type CellKind = 'text' | 'number' | 'boolean' | 'error';

/** One cell as the page shows it. */
export interface SheetCell {
	/** What the sheet shows, such as a number as its format writes it. */
	text: string;
	kind: CellKind;
	/** The formula without its "=", if the cell has one. */
	formula?: string;
}

export interface Pos {
	r: number;
	c: number;
}

export interface Grid {
	/** The rows and columns shown. */
	rows: number;
	cols: number;
	/** The rows and columns the sheet has. */
	totalRows: number;
	totalCols: number;
	/** Row by row. A row can be shorter than `cols`, and an empty cell is undefined. */
	cells: Array<Array<SheetCell | undefined>>;
	/** Pixels, one per column shown. */
	widths: number[];
	/** How far each merged range spans, by the index of its top left cell. */
	spans: Map<number, { r: number; c: number }>;
	/** Where the merged range starts, by the index of each cell under it. */
	covered: Map<number, Pos>;
}

/** A cell's place in `spans` and `covered`, a number so that looking one up allocates nothing. */
export function cellIndex(cols: number, r: number, c: number): number {
	return r * cols + c;
}

/** A column's letters as a sheet writes them: 0 is A, 25 is Z, 26 is AA. */
export function colName(c: number): string {
	let name = '';
	for (let n = c + 1; n > 0; n = Math.floor((n - 1) / 26)) {
		name = String.fromCharCode(65 + ((n - 1) % 26)) + name;
	}
	return name;
}

/** A cell's address, such as B7. */
export function cellName({ r, c }: Pos): string {
	return `${colName(c)}${r + 1}`;
}

/** Whether a file is plain delimited text, which is read differently from a workbook. */
export function isDelimited(name: string): boolean {
	const ext = extOf(name);
	return ext === 'csv' || ext === 'tsv';
}

/**
 * Text from a file's bytes. Excel's "Unicode text" is UTF-16 and says so in a byte order mark.
 * Text that isn't valid UTF-8 is taken to be what Windows exports in the West.
 */
export function decodeText(data: ArrayBuffer | Uint8Array): string {
	const bytes = data instanceof Uint8Array ? data : new Uint8Array(data);
	if (bytes[0] === 0xff && bytes[1] === 0xfe) {
		return new TextDecoder('utf-16le').decode(bytes);
	}
	if (bytes[0] === 0xfe && bytes[1] === 0xff) {
		return new TextDecoder('utf-16be').decode(bytes);
	}
	try {
		return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
	} catch {
		return new TextDecoder('windows-1252').decode(bytes);
	}
}

const BOOK_OPTIONS: ParsingOptions = {
	type: 'array',
	dense: true,
	cellFormula: true,
	cellStyles: false,
	cellHTML: false
};

/** Reads a workbook, or a CSV or TSV file as a workbook of one sheet. Throws if it's unreadable. */
export function parseWorkbook(data: ArrayBuffer | Uint8Array, name: string): WorkBook {
	if (isDelimited(name)) {
		// SheetJS guesses the delimiter from the first line, and takes a tab file for commas when
		// the text has more of them, so a tab file has to say so. UTF-8 without a byte order mark
		// would be read as Latin-1, so the text is decoded here.
		return read(decodeText(data), {
			type: 'string',
			dense: true,
			raw: false,
			...(extOf(name) === 'tsv' ? { FS: '\t' } : {})
		});
	}
	// Past the rows the page shows, SheetJS can skip the cells instead of building them, which
	// saves most of the time on a huge sheet. Only the binary formats then say how big the sheet
	// was (`!fullref`), so a sheet cut at the limit without it is read again in full.
	const capped = read(data, { ...BOOK_OPTIONS, sheetRows: MAX_ROWS });
	return capped.SheetNames.some((n) => cutBlind(capped.Sheets[n]))
		? read(data, BOOK_OPTIONS)
		: capped;
}

function cutBlind(ws: WorkSheet | undefined): boolean {
	return (
		!!ws && !ws['!fullref'] && !!ws['!ref'] && utils.decode_range(ws['!ref']).e.r + 1 >= MAX_ROWS
	);
}

/** What a cell shows: its formatted text, else its value, else its formula. */
export function cellText(cell: CellObject | undefined): string {
	if (!cell) {
		return '';
	}
	if (cell.w != null) {
		return cell.w;
	}
	if (cell.v == null) {
		return cell.f ? `=${cell.f}` : '';
	}
	return cell.v instanceof Date ? cell.v.toLocaleString() : String(cell.v);
}

function toCell(cell: CellObject | undefined): SheetCell | undefined {
	const text = cellText(cell);
	if (!cell || (text === '' && !cell.f)) {
		return undefined;
	}
	const kind: CellKind =
		cell.t === 'n' || cell.t === 'd'
			? 'number'
			: cell.t === 'b'
				? 'boolean'
				: cell.t === 'e'
					? 'error'
					: 'text';
	return { text, kind, formula: cell.f };
}

const clamp = (n: number, min: number, max: number) => Math.min(max, Math.max(min, n));

/** A column's width in pixels, from the file's width if it has one, else from its longest text. */
export function columnWidth(
	spec: Pick<ColInfo, 'wpx' | 'wch'> | undefined,
	longest: number
): number {
	if (spec?.wpx) {
		return clamp(spec.wpx, 40, 420);
	}
	if (spec?.wch) {
		return clamp(spec.wch * 7.5 + 12, 40, 420);
	}
	return clamp(longest * 7 + 18, 64, 320);
}

/** How many rows to show of a sheet that has this many columns. */
export function rowLimit(totalRows: number, cols: number): number {
	return Math.min(
		totalRows,
		MAX_ROWS,
		Math.max(MIN_ROWS, Math.floor(MAX_CELLS / Math.max(cols, 1)))
	);
}

/** The grid to show for a sheet, cut to the limits above. */
export function buildGrid(ws: WorkSheet | undefined): Grid {
	// A sheet read with sheetRows keeps its real size in !fullref.
	const ref = ws?.['!fullref'] ?? ws?.['!ref'];
	const range = ref ? utils.decode_range(ref) : { s: { r: 0, c: 0 }, e: { r: -1, c: -1 } };
	const totalRows = range.e.r + 1;
	const totalCols = range.e.c + 1;
	const cols = Math.min(totalCols, MAX_COLS);
	const rows = rowLimit(totalRows, cols);

	const dense = (ws?.['!data'] ?? []) as Array<Array<CellObject | undefined> | undefined>;
	const cells: Grid['cells'] = [];
	for (let r = 0; r < rows; r++) {
		const source = dense[r] ?? [];
		const row: Array<SheetCell | undefined> = [];
		for (let c = 0; c < Math.min(source.length, cols); c++) {
			row.push(toCell(source[c]));
		}
		cells.push(row);
	}

	const widths: number[] = [];
	for (let c = 0; c < cols; c++) {
		let longest = 0;
		for (let r = 0; r < Math.min(rows, WIDTH_SAMPLE); r++) {
			longest = Math.max(longest, cells[r][c]?.text.length ?? 0);
		}
		widths.push(columnWidth(ws?.['!cols']?.[c], longest));
	}

	const spans: Grid['spans'] = new Map();
	const covered: Grid['covered'] = new Map();
	for (const m of ws?.['!merges'] ?? []) {
		if (m.s.r >= rows || m.s.c >= cols) {
			continue;
		}
		const endR = Math.min(m.e.r, rows - 1);
		const endC = Math.min(m.e.c, cols - 1);
		spans.set(cellIndex(cols, m.s.r, m.s.c), { r: endR - m.s.r + 1, c: endC - m.s.c + 1 });
		for (let r = m.s.r; r <= endR; r++) {
			for (let c = m.s.c; c <= endC; c++) {
				if (r !== m.s.r || c !== m.s.c) {
					covered.set(cellIndex(cols, r, c), { r: m.s.r, c: m.s.c });
				}
			}
		}
	}
	return { rows, cols, totalRows, totalCols, cells, widths, spans, covered };
}

/** Tells the reader a sheet was cut short, or returns null if all of it is shown. */
export function truncationNote(grid: Grid): string | null {
	const parts: string[] = [];
	if (grid.rows < grid.totalRows) {
		parts.push(
			`the first ${grid.rows.toLocaleString()} of ${grid.totalRows.toLocaleString()} rows`
		);
	}
	if (grid.cols < grid.totalCols) {
		parts.push(
			`${grid.rows < grid.totalRows ? 'the first ' : ''}${grid.cols.toLocaleString()} of ${grid.totalCols.toLocaleString()} columns`
		);
	}
	return parts.length
		? `Showing ${parts.join(' and ')}. Download the file to see everything.`
		: null;
}

/** The cell to select when a merged range is hit anywhere: the one it starts at. */
export function originOf(grid: Pick<Grid, 'cols' | 'covered'>, r: number, c: number): Pos {
	return grid.covered.get(cellIndex(grid.cols, r, c)) ?? { r, c };
}

export type Move =
	| 'up'
	| 'down'
	| 'left'
	| 'right'
	| 'rowStart'
	| 'rowEnd'
	| 'first'
	| 'last'
	| 'pageUp'
	| 'pageDown';

/** What a key does to the selection, or undefined if it does nothing. */
export function moveFor(key: string, ctrl: boolean): Move | undefined {
	switch (key) {
		case 'ArrowUp':
			return 'up';
		case 'ArrowDown':
			return 'down';
		case 'ArrowLeft':
			return 'left';
		case 'ArrowRight':
			return 'right';
		case 'Home':
			return ctrl ? 'first' : 'rowStart';
		case 'End':
			return ctrl ? 'last' : 'rowEnd';
		case 'PageUp':
			return 'pageUp';
		case 'PageDown':
			return 'pageDown';
	}
}

/** Where the selection goes, which stays in the grid and never lands under a merged range. */
export function moveSelection(grid: Grid, from: Pos, move: Move, page = 10): Pos {
	// A merged range is one cell, so moving off its far edge skips the rest of it.
	const span = grid.spans.get(cellIndex(grid.cols, from.r, from.c));
	let { r, c } = from;
	switch (move) {
		case 'up':
			r--;
			break;
		case 'down':
			r += span?.r ?? 1;
			break;
		case 'left':
			c--;
			break;
		case 'right':
			c += span?.c ?? 1;
			break;
		case 'rowStart':
			c = 0;
			break;
		case 'rowEnd':
			c = grid.cols - 1;
			break;
		case 'first':
			r = 0;
			c = 0;
			break;
		case 'last':
			r = grid.rows - 1;
			c = grid.cols - 1;
			break;
		case 'pageUp':
			r -= page;
			break;
		case 'pageDown':
			r += page;
			break;
	}
	return originOf(grid, clamp(r, 0, grid.rows - 1), clamp(c, 0, grid.cols - 1));
}
