import { describe, expect, test } from 'bun:test';
import { color256, contrast, palette, parseAnsi, plainText } from './ansi.js';

const esc = '\x1b';

describe('plainText', () => {
	const cases: { name: string; input: string; want: string }[] = [
		{ name: 'plain text is untouched', input: 'GET /health 200', want: 'GET /health 200' },
		{ name: 'colors are dropped', input: `${esc}[31mred${esc}[0m text`, want: 'red text' },
		{
			name: 'cursor moves and clears are dropped',
			input: `a${esc}[2K${esc}[1Gb${esc}[?25lc`,
			want: 'abc'
		},
		{
			name: 'a window title ended by BEL is dropped',
			input: `${esc}]0;my title\x07done`,
			want: 'done'
		},
		{
			name: 'a window title ended by ESC \\ is dropped',
			input: `${esc}]2;title${esc}\\done`,
			want: 'done'
		},
		{ name: 'a two-byte escape is dropped', input: `a${esc}Mb${esc}7c`, want: 'abc' },
		{ name: 'a character set choice is dropped', input: `a${esc}(Bb${esc}=c`, want: 'abc' },
		{ name: 'BEL, backspace and NUL are dropped', input: 'a\x07b\x08c\x00d', want: 'abcd' },
		{ name: 'tabs stay', input: 'a\tb', want: 'a\tb' },
		{ name: 'a lone ESC at the end is dropped', input: `abc${esc}`, want: 'abc' },
		{ name: 'a color sequence cut off at the end is dropped', input: `abc${esc}[3`, want: 'abc' },
		{ name: 'a color sequence cut off after ESC [ is dropped', input: `abc${esc}[`, want: 'abc' },
		{ name: 'a title cut off at the end is dropped', input: `abc${esc}]0;ti`, want: 'abc' },
		{ name: 'a progress bar keeps its last state', input: '10%\r50%\r100%', want: '100%' },
		{ name: 'a trailing return keeps the text before it', input: 'done\r', want: 'done' },
		{ name: 'a clear after a return does not blank the line', input: `x\r${esc}[K`, want: 'x' },
		{
			name: 'non-ASCII text stays',
			input: `${esc}[32m✓ 日本語 ünïcode${esc}[0m`,
			want: '✓ 日本語 ünïcode'
		},
		{ name: 'empty', input: '', want: '' }
	];
	for (const c of cases) {
		test(c.name, () => {
			expect(plainText(c.input)).toBe(c.want);
		});
	}
});

describe('parseAnsi', () => {
	test('plain text is one segment with no style', () => {
		expect(parseAnsi('hello')).toEqual([{ text: 'hello', css: '' }]);
	});

	test('empty text has no segments', () => {
		expect(parseAnsi('')).toEqual([]);
	});

	test('a basic color applies until it is reset', () => {
		expect(parseAnsi(`a ${esc}[31mred${esc}[0m b`)).toEqual([
			{ text: 'a ', css: '' },
			{ text: 'red', css: `color:${palette[1]}` },
			{ text: ' b', css: '' }
		]);
	});

	test('bold, dim, italic and underline', () => {
		const segs = parseAnsi(
			`${esc}[1mb${esc}[22m${esc}[2md${esc}[22m${esc}[3mi${esc}[23m${esc}[4mu${esc}[24mn`
		);
		expect(segs.map((s) => s.css)).toEqual([
			'font-weight:600',
			'opacity:0.65',
			'font-style:italic',
			'text-decoration:underline',
			''
		]);
	});

	test('strike-through joins underline', () => {
		expect(parseAnsi(`${esc}[4;9mx`)[0].css).toBe('text-decoration:underline line-through');
	});

	test('bright colors come from the second half of the palette', () => {
		expect(parseAnsi(`${esc}[92mg`)[0].css).toBe(`color:${palette[10]}`);
		expect(parseAnsi(`${esc}[101m x`)[0].css).toContain(`background-color:${palette[9]}`);
	});

	test('an empty SGR resets', () => {
		expect(parseAnsi(`${esc}[31ma${esc}[mb`).map((s) => s.css)).toEqual([
			`color:${palette[1]}`,
			''
		]);
	});

	test('39 and 49 return to the default colors', () => {
		expect(parseAnsi(`${esc}[31;44ma${esc}[39;49mb`).map((s) => s.css)).toEqual([
			expect.stringContaining('background-color:'),
			''
		]);
	});

	test('256 colors', () => {
		expect(parseAnsi(`${esc}[38;5;208mx`)[0].css).toBe('color:#ff8700');
		expect(parseAnsi(`${esc}[38;5;9mx`)[0].css).toBe(`color:${palette[9]}`);
		expect(parseAnsi(`${esc}[38;5;255mx`)[0].css).toBe('color:#eeeeee');
		expect(parseAnsi(`${esc}[48;5;21mx`)[0].css).toContain('background-color:#0000ff');
	});

	test('true color, in semicolon and colon forms', () => {
		expect(parseAnsi(`${esc}[38;2;255;128;0mx`)[0].css).toBe('color:#ff8000');
		expect(parseAnsi(`${esc}[38:2:255:128:0mx`)[0].css).toBe('color:#ff8000');
		expect(parseAnsi(`${esc}[38:2::255:128:0mx`)[0].css).toBe('color:#ff8000');
		expect(parseAnsi(`${esc}[38:5:208mx`)[0].css).toBe('color:#ff8700');
	});

	test('true color values are clamped to a byte', () => {
		expect(parseAnsi(`${esc}[38;2;999;300;0mx`)[0].css).toBe('color:#ffff00');
	});

	test('a color followed by more codes keeps reading them', () => {
		expect(parseAnsi(`${esc}[38;5;208;1mx`)[0].css).toBe('color:#ff8700;font-weight:600');
		expect(parseAnsi(`${esc}[1;38;2;0;255;0;4mx`)[0].css).toBe(
			'color:#00ff00;font-weight:600;text-decoration:underline'
		);
	});

	test('a color mode that is not 5 or 2 ends the sequence', () => {
		expect(parseAnsi(`${esc}[1;38;9;4mx`)[0].css).toBe('font-weight:600');
	});

	test('dark colors are lifted so they can be read on the dark background', () => {
		const css = parseAnsi(`${esc}[38;2;0;0;100mx`)[0].css;
		const color = /^color:(#[0-9a-f]{6})$/.exec(css)?.[1];
		expect(color).toBeDefined();
		expect(contrast(color!, '#000000')).toBeGreaterThanOrEqual(3);
		expect(color).not.toBe('#000064');
	});

	test('colors that can already be read are not changed', () => {
		expect(parseAnsi(`${esc}[38;2;240;200;10mx`)[0].css).toBe('color:#f0c80a');
	});

	test('text on a background with no color of its own is made readable', () => {
		// The default text is light, and white is no better on #d4d4d4.
		const css = parseAnsi(`${esc}[47mx`)[0].css;
		const fg = /color:(#[0-9a-f]{6})/.exec(css)?.[1];
		expect(fg).toBeDefined();
		expect(contrast(fg!, palette[7])).toBeGreaterThanOrEqual(3);
	});

	test('inverse swaps the colors', () => {
		const css = parseAnsi(`${esc}[31;7mx`)[0].css;
		expect(css).toContain(`background-color:${palette[1]}`);
		expect(css).toMatch(/^color:#[0-9a-f]{6};/);
	});

	test('merges runs that end up with the same style', () => {
		expect(parseAnsi(`a${esc}[31m${esc}[31mb${esc}[0mc${esc}[0md`)).toEqual([
			{ text: 'a', css: '' },
			{ text: 'b', css: `color:${palette[1]}` },
			{ text: 'cd', css: '' }
		]);
	});

	test('other sequences between colors do not break the style', () => {
		expect(parseAnsi(`${esc}[31ma${esc}[2Kb`)).toEqual([
			{ text: 'ab', css: `color:${palette[1]}` }
		]);
	});

	test('private-mode sequences that end in m are not colors', () => {
		expect(parseAnsi(`${esc}[?1mx`)).toEqual([{ text: 'x', css: '' }]);
	});

	test('a sequence cut off at the end leaves no garbage', () => {
		expect(parseAnsi(`ok ${esc}[3`)).toEqual([{ text: 'ok ', css: '' }]);
		expect(parseAnsi(`${esc}[31mred ${esc}[`)).toEqual([
			{ text: 'red ', css: `color:${palette[1]}` }
		]);
	});

	test('the rest of a cut-off sequence at the start of the next line shows as text', () => {
		// Nothing can be done about it without carrying state from line to line.
		expect(parseAnsi('1mbold')).toEqual([{ text: '1mbold', css: '' }]);
	});

	test('markup in the text stays text', () => {
		const segs = parseAnsi(`${esc}[31m<img src=x onerror=alert(1)>${esc}[0m`);
		expect(segs).toEqual([{ text: '<img src=x onerror=alert(1)>', css: `color:${palette[1]}` }]);
	});

	test('the style never carries anything but known properties', () => {
		const hostile = `${esc}[38;2;1;2;3mx${esc}[48;5;300my${esc}[38;5;x;1mz`;
		for (const s of parseAnsi(hostile)) {
			expect(s.css).toMatch(/^([a-z-]+:[#a-z0-9. -]+;?)*$/);
		}
	});

	const joined: string[] = [
		'plain',
		`${esc}[1;31mfatal${esc}[0m: ${esc}[4mdisk${esc}[24m full`,
		`${esc}]0;t\x07${esc}[2Kprogress 10%\rprogress 20%`,
		`a${esc}[`,
		`\x07bell${esc}`,
		`${esc}[38;2;1;2;3m${esc}[48;5;200mmulti${esc}[m`
	];
	for (const input of joined) {
		test(`the segments of ${JSON.stringify(input)} add up to its plain text`, () => {
			expect(
				parseAnsi(input)
					.map((s) => s.text)
					.join('')
			).toBe(plainText(input));
		});
	}
});

describe('color256', () => {
	const cases: [number, string | null][] = [
		[0, palette[0]],
		[15, palette[15]],
		[16, '#000000'],
		[21, '#0000ff'],
		[196, '#ff0000'],
		[231, '#ffffff'],
		[232, '#080808'],
		[255, '#eeeeee'],
		[256, null],
		[-1, null],
		[1.5, null],
		[NaN, null]
	];
	for (const [n, want] of cases) {
		test(`${n}`, () => {
			expect(color256(n)).toBe(want);
		});
	}
});
