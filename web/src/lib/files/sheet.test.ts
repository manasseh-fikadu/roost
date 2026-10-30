import { describe, expect, test } from 'bun:test';
import { utils, write, type BookType, type WorkSheet } from 'xlsx';
import {
	MAX_CELLS,
	MAX_COLS,
	MAX_ROWS,
	buildGrid,
	cellName,
	cellText,
	colName,
	columnWidth,
	decodeText,
	isDelimited,
	moveFor,
	moveSelection,
	originOf,
	parseWorkbook,
	rowLimit,
	truncationNote,
	type Grid
} from './sheet.js';

const encode = (s: string) => new TextEncoder().encode(s);

function sheetOf(rows: unknown[][]): WorkSheet {
	return utils.aoa_to_sheet(rows, { dense: true });
}

function workbookBytes(sheets: Record<string, unknown[][]>, bookType: BookType = 'xlsx') {
	const wb = utils.book_new();
	for (const [name, rows] of Object.entries(sheets)) {
		utils.book_append_sheet(wb, sheetOf(rows), name);
	}
	return write(wb, { type: 'array', bookType }) as ArrayBuffer;
}

function numbered(rows: number, cols = 2): number[][] {
	return Array.from({ length: rows }, (_, r) => Array.from({ length: cols }, (_, c) => r * 10 + c));
}

describe('colName and cellName', () => {
	test.each([
		[0, 'A'],
		[1, 'B'],
		[25, 'Z'],
		[26, 'AA'],
		[27, 'AB'],
		[51, 'AZ'],
		[52, 'BA'],
		[701, 'ZZ'],
		[702, 'AAA'],
		[16383, 'XFD']
	])('column %i is %s', (c, name) => {
		expect(colName(c)).toBe(name);
	});

	test('a cell is its column and its one-based row', () => {
		expect(cellName({ r: 0, c: 0 })).toBe('A1');
		expect(cellName({ r: 6, c: 27 })).toBe('AB7');
	});
});

test('isDelimited is true for CSV and TSV in any case', () => {
	expect(isDelimited('data.csv')).toBe(true);
	expect(isDelimited('DATA.TSV')).toBe(true);
	expect(isDelimited('data.xlsx')).toBe(false);
	expect(isDelimited('csv')).toBe(false);
	expect(isDelimited('notes.csv.bak')).toBe(false);
});

describe('decodeText', () => {
	test('UTF-8 with and without a byte order mark', () => {
		expect(decodeText(encode('café ☃'))).toBe('café ☃');
		expect(decodeText(new Uint8Array([0xef, 0xbb, 0xbf, ...encode('a,b')]))).toBe('a,b');
	});

	test('UTF-16 by its byte order mark', () => {
		const le = new Uint8Array([0xff, 0xfe, 0x61, 0x00, 0x09, 0x00, 0x62, 0x00]);
		const be = new Uint8Array([0xfe, 0xff, 0x00, 0x61, 0x00, 0x09, 0x00, 0x62]);
		expect(decodeText(le)).toBe('a\tb');
		expect(decodeText(be)).toBe('a\tb');
	});

	test('bytes that are not UTF-8 are Windows Latin-1', () => {
		expect(decodeText(new Uint8Array([0x63, 0x61, 0x66, 0xe9]))).toBe('café');
	});

	test('takes an ArrayBuffer', () => {
		expect(decodeText(encode('x').buffer as ArrayBuffer)).toBe('x');
	});
});

describe('parseWorkbook', () => {
	test('reads a CSV into one sheet with typed cells', () => {
		const book = parseWorkbook(encode('name,qty\nbolt,12\nnut,"1,5"\n'), 'parts.csv');
		expect(book.SheetNames).toHaveLength(1);
		const grid = buildGrid(book.Sheets[book.SheetNames[0]]);
		expect(grid.totalRows).toBe(3);
		expect(grid.cells[0].map((c) => c?.text)).toEqual(['name', 'qty']);
		expect(grid.cells[1][1]).toEqual({ text: '12', kind: 'number', formula: undefined });
		expect(grid.cells[2][1]?.text).toBe('1,5');
	});

	test('reads a TSV by tabs even when the text has more commas', () => {
		const book = parseWorkbook(encode('name\tnote\nbob\ta, b, c, d\n'), 'people.tsv');
		const grid = buildGrid(book.Sheets[book.SheetNames[0]]);
		expect(grid.totalCols).toBe(2);
		expect(grid.cells[1].map((c) => c?.text)).toEqual(['bob', 'a, b, c, d']);
	});

	test('reads a semicolon CSV', () => {
		const book = parseWorkbook(encode('a;b;c\n1;2;3\n'), 'eu.csv');
		expect(buildGrid(book.Sheets[book.SheetNames[0]]).totalCols).toBe(3);
	});

	test('decodes UTF-8 CSV without a byte order mark as UTF-8', () => {
		const book = parseWorkbook(encode('naïve\n'), 'x.csv');
		expect(buildGrid(book.Sheets[book.SheetNames[0]]).cells[0][0]?.text).toBe('naïve');
	});

	test('reads every sheet of a workbook, with formulas', () => {
		const wb = utils.book_new();
		const first = sheetOf([[1, 2]]);
		first['!data']![0][1] = { t: 'n', v: 3, f: 'A1+2' };
		utils.book_append_sheet(wb, first, 'First');
		utils.book_append_sheet(wb, sheetOf([['x']]), 'Second');
		const bytes = write(wb, { type: 'array', bookType: 'xlsx' }) as ArrayBuffer;

		const book = parseWorkbook(bytes, 'two.xlsx');
		expect(book.SheetNames).toEqual(['First', 'Second']);
		const grid = buildGrid(book.Sheets.First);
		expect(grid.cells[0][1]?.formula).toBe('A1+2');
		expect(grid.cells[0][1]?.text).toBe('3');
	});

	test('keeps the real size of a sheet it stops reading at the row limit', () => {
		const book = parseWorkbook(workbookBytes({ Big: numbered(MAX_ROWS + 1000) }), 'big.xlsx');
		const grid = buildGrid(book.Sheets.Big);
		expect(grid.totalRows).toBe(MAX_ROWS + 1000);
		expect(grid.rows).toBe(MAX_ROWS);
		expect(grid.cells).toHaveLength(MAX_ROWS);
		expect(truncationNote(grid)).toContain(`${(MAX_ROWS + 1000).toLocaleString()} rows`);
	});

	test('reads a legacy .xls too', () => {
		const book = parseWorkbook(workbookBytes({ Old: [['a', 1]] }, 'biff8'), 'old.xls');
		expect(buildGrid(book.Sheets.Old).cells[0][0]?.text).toBe('a');
	});

	test('reads in full a sheet that cannot say how big it was', () => {
		// Excel's HTML tables are often saved as .xls, and SheetJS doesn't record the full size of those.
		const rows = Array.from({ length: MAX_ROWS + 50 }, (_, r) => `<tr><td>${r}</td></tr>`);
		const html = `<table>${rows.join('')}</table>`;
		const book = parseWorkbook(encode(html), 'export.xls');
		const grid = buildGrid(book.Sheets[book.SheetNames[0]]);
		expect(grid.totalRows).toBe(MAX_ROWS + 50);
		expect(truncationNote(grid)).not.toBeNull();
	});

	test('throws on a workbook that is cut short', () => {
		const bytes = new Uint8Array(workbookBytes({ A: numbered(50) }));
		expect(() => parseWorkbook(bytes.slice(0, bytes.length / 2), 'cut.xlsx')).toThrow();
	});
});

describe('cellText', () => {
	test('prefers the formatted text, then the value, then the formula', () => {
		expect(cellText(undefined)).toBe('');
		expect(cellText({ t: 'n', v: 0.5, w: '50%' })).toBe('50%');
		expect(cellText({ t: 'n', v: 7 })).toBe('7');
		expect(cellText({ t: 'b', v: true })).toBe('true');
		expect(cellText({ t: 'z', f: 'SUM(A1:A3)' })).toBe('=SUM(A1:A3)');
		expect(cellText({ t: 'z' })).toBe('');
	});
});

describe('buildGrid', () => {
	test('a missing or empty sheet has no rows', () => {
		for (const ws of [undefined, {} as WorkSheet]) {
			const grid = buildGrid(ws);
			expect(grid.rows).toBe(0);
			expect(grid.cols).toBe(0);
			expect(grid.cells).toEqual([]);
			expect(truncationNote(grid)).toBeNull();
		}
	});

	test('classifies cells and drops the empty ones', () => {
		const ws = sheetOf([['text', 1, true, '', null]]);
		ws['!data']![0][5] = { t: 'e', v: 7, w: '#DIV/0!' };
		ws['!ref'] = 'A1:F1';
		const row = buildGrid(ws).cells[0];
		expect(row.map((c) => c?.kind)).toEqual([
			'text',
			'number',
			'boolean',
			undefined,
			undefined,
			'error'
		]);
	});

	test('a short row is not padded', () => {
		const grid = buildGrid(sheetOf([[1, 2, 3], [1]]));
		expect(grid.cells[1]).toHaveLength(1);
		expect(grid.cells[1][2]).toBeUndefined();
	});

	test('cuts wide sheets to the column limit and fewer rows', () => {
		const grid = buildGrid(sheetOf(numbered(1000, MAX_COLS + 50)));
		expect(grid.totalCols).toBe(MAX_COLS + 50);
		expect(grid.cols).toBe(MAX_COLS);
		expect(grid.widths).toHaveLength(MAX_COLS);
		expect(grid.rows).toBe(Math.floor(MAX_CELLS / MAX_COLS));
		expect(grid.cells[0]).toHaveLength(MAX_COLS);
		expect(truncationNote(grid)).toBe(
			`Showing the first ${grid.rows.toLocaleString()} of 1,000 rows and the first ${MAX_COLS} of ${MAX_COLS + 50} columns. Download the file to see everything.`
		);
	});

	test('sizes columns from the file, else from the longest text', () => {
		const ws = sheetOf([['a', 'a much longer piece of text', 'b']]);
		ws['!cols'] = [{ wpx: 100 }, undefined as never, { wch: 10 }];
		const { widths } = buildGrid(ws);
		expect(widths[0]).toBe(100);
		expect(widths[1]).toBe('a much longer piece of text'.length * 7 + 18);
		expect(widths[2]).toBe(10 * 7.5 + 12);
	});

	test('the row of a merged range owns it and the cells under it are covered', () => {
		const ws = sheetOf(numbered(4, 4));
		ws['!merges'] = [{ s: { r: 1, c: 1 }, e: { r: 2, c: 3 } }];
		const grid = buildGrid(ws);
		expect(grid.spans.get(1 * 4 + 1)).toEqual({ r: 2, c: 3 });
		expect([...grid.covered.keys()].sort((a, b) => a - b)).toEqual([6, 7, 9, 10, 11]);
		expect(originOf(grid, 2, 3)).toEqual({ r: 1, c: 1 });
		expect(originOf(grid, 0, 0)).toEqual({ r: 0, c: 0 });
	});

	test('a merged range is clipped to what is shown, and one past it is dropped', () => {
		const ws = sheetOf(numbered(3, 3));
		ws['!merges'] = [
			{ s: { r: 1, c: 1 }, e: { r: 50, c: 50 } },
			{ s: { r: 40, c: 0 }, e: { r: 41, c: 0 } }
		];
		const grid = buildGrid(ws);
		expect(grid.spans.size).toBe(1);
		expect(grid.spans.get(1 * 3 + 1)).toEqual({ r: 2, c: 2 });
	});
});

test('rowLimit trades rows for columns, but never below the minimum', () => {
	expect(rowLimit(10, 5)).toBe(10);
	expect(rowLimit(1_000_000, 5)).toBe(MAX_ROWS);
	expect(rowLimit(1_000_000, 100)).toBe(800);
	expect(rowLimit(1_000_000, MAX_COLS)).toBe(400);
	expect(rowLimit(1_000_000, 10_000)).toBe(200);
	expect(rowLimit(50, 10_000)).toBe(50);
	expect(rowLimit(0, 0)).toBe(0);
});

test('columnWidth stays between its bounds', () => {
	expect(columnWidth({ wpx: 5 }, 0)).toBe(40);
	expect(columnWidth({ wpx: 9000 }, 0)).toBe(420);
	expect(columnWidth({ wch: 1 }, 0)).toBe(40);
	expect(columnWidth(undefined, 0)).toBe(64);
	expect(columnWidth(undefined, 1000)).toBe(320);
});

describe('truncationNote', () => {
	const grid = (over: Partial<Grid>): Grid => ({
		rows: 10,
		cols: 4,
		totalRows: 10,
		totalCols: 4,
		cells: [],
		widths: [],
		spans: new Map(),
		covered: new Map(),
		...over
	});

	test('is null when everything is shown', () => {
		expect(truncationNote(grid({}))).toBeNull();
	});

	test('names rows, columns, or both', () => {
		expect(truncationNote(grid({ totalRows: 2500 }))).toBe(
			`Showing the first 10 of ${(2500).toLocaleString()} rows. Download the file to see everything.`
		);
		expect(truncationNote(grid({ totalCols: 9 }))).toBe(
			'Showing 4 of 9 columns. Download the file to see everything.'
		);
		expect(truncationNote(grid({ totalRows: 11, totalCols: 9 }))).toBe(
			'Showing the first 10 of 11 rows and the first 4 of 9 columns. Download the file to see everything.'
		);
	});
});

describe('keyboard moves', () => {
	const merged = () => {
		const ws = sheetOf(numbered(5, 5));
		ws['!merges'] = [{ s: { r: 1, c: 1 }, e: { r: 2, c: 2 } }];
		return buildGrid(ws);
	};

	test('moveFor maps keys, and Ctrl turns Home and End into the corners', () => {
		expect(moveFor('ArrowDown', false)).toBe('down');
		expect(moveFor('Home', false)).toBe('rowStart');
		expect(moveFor('Home', true)).toBe('first');
		expect(moveFor('End', true)).toBe('last');
		expect(moveFor('PageDown', false)).toBe('pageDown');
		expect(moveFor('a', false)).toBeUndefined();
		expect(moveFor('Enter', false)).toBeUndefined();
	});

	test('arrows step one cell and stop at the edges', () => {
		const grid = buildGrid(sheetOf(numbered(3, 3)));
		expect(moveSelection(grid, { r: 0, c: 0 }, 'up')).toEqual({ r: 0, c: 0 });
		expect(moveSelection(grid, { r: 0, c: 0 }, 'left')).toEqual({ r: 0, c: 0 });
		expect(moveSelection(grid, { r: 0, c: 0 }, 'right')).toEqual({ r: 0, c: 1 });
		expect(moveSelection(grid, { r: 2, c: 2 }, 'down')).toEqual({ r: 2, c: 2 });
		expect(moveSelection(grid, { r: 2, c: 2 }, 'right')).toEqual({ r: 2, c: 2 });
	});

	test('Home, End, the corners and pages', () => {
		const grid = buildGrid(sheetOf(numbered(30, 6)));
		const at = { r: 12, c: 3 };
		expect(moveSelection(grid, at, 'rowStart')).toEqual({ r: 12, c: 0 });
		expect(moveSelection(grid, at, 'rowEnd')).toEqual({ r: 12, c: 5 });
		expect(moveSelection(grid, at, 'first')).toEqual({ r: 0, c: 0 });
		expect(moveSelection(grid, at, 'last')).toEqual({ r: 29, c: 5 });
		expect(moveSelection(grid, at, 'pageDown')).toEqual({ r: 22, c: 3 });
		expect(moveSelection(grid, at, 'pageDown', 25)).toEqual({ r: 29, c: 3 });
		expect(moveSelection(grid, at, 'pageUp', 20)).toEqual({ r: 0, c: 3 });
	});

	test('moving off a merged range skips the rest of it', () => {
		const grid = merged();
		expect(moveSelection(grid, { r: 1, c: 1 }, 'right')).toEqual({ r: 1, c: 3 });
		expect(moveSelection(grid, { r: 1, c: 1 }, 'down')).toEqual({ r: 3, c: 1 });
	});

	test('moving into a merged range lands on where it starts', () => {
		const grid = merged();
		expect(moveSelection(grid, { r: 1, c: 3 }, 'left')).toEqual({ r: 1, c: 1 });
		expect(moveSelection(grid, { r: 3, c: 2 }, 'up')).toEqual({ r: 1, c: 1 });
		expect(moveSelection(grid, { r: 2, c: 0 }, 'right')).toEqual({ r: 1, c: 1 });
	});
});
