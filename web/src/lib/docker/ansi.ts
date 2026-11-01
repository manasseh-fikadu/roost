// Terminal text as safe pieces. A container's output has colors and other
// escape codes in it. This turns the colors into inline styles from a fixed
// palette, and drops every other escape code and control character, so a
// line can be shown as plain text in <span>s and never as HTML.

/** A run of text with one style. `css` is empty for the default style. */
export interface AnsiSegment {
	text: string;
	css: string;
}

/** The 16 terminal colors, tuned for Bield's dark theme: black is lifted so it stays visible. */
export const palette = [
	'#5c5c5c',
	'#ff6166',
	'#62c073',
	'#f1a10d',
	'#52a8ff',
	'#bf7af0',
	'#4fc6d6',
	'#d4d4d4',
	'#8f8f8f',
	'#ff8a8d',
	'#86d895',
	'#ffc35a',
	'#85c1ff',
	'#d4a4f6',
	'#7ddbe6',
	'#ffffff'
];

/** The text and background of the log, for working out what is readable. */
const defaultFg = '#ededed';
const defaultBg = '#000000';
/** How much lighter than its background a color must be. WCAG asks 3:1 for large text and UI. */
const minContrast = 3;

/**
 * Everything that isn't text, in the order to try it: a color or other CSI
 * sequence, a complete OSC string (such as a window title), a sequence the
 * line ends in the middle of, any other escape (such as ESC 7 to save the
 * cursor, or ESC ( B to pick a character set), and the control
 * characters that would show as boxes. The sequences are complete or
 * dropped, so a line cut off in the middle of one leaves no garbage.
 */
const TOKEN =
	/\x1b\[([0-?]*)[ -/]*([@-~])|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b(?:\[[0-?]*[ -/]*|\][^\x07\x1b]*)?$|\x1b[ -/]*[0-~]|[\x00-\x08\x0b-\x1f\x7f]/g;
const CONTROL = /[\x00-\x08\x0b-\x1f\x7f]/;

/**
 * What a terminal shows of a line with carriage returns: a progress bar
 * writes "10%", goes back to the start, and writes "20%" over it. This keeps
 * the last part that has any text.
 */
function afterReturns(input: string): string {
	if (!input.includes('\r')) {
		return input;
	}
	const parts = input.split('\r');
	for (let i = parts.length - 1; i >= 0; i--) {
		if (parts[i].replace(TOKEN, '') !== '') {
			return parts[i];
		}
	}
	return '';
}

/** The text of a line with every escape code and control character gone. */
export function plainText(input: string): string {
	const text = afterReturns(input);
	return CONTROL.test(text) ? text.replace(TOKEN, '') : text;
}

interface Style {
	fg: string | null;
	bg: string | null;
	bold: boolean;
	dim: boolean;
	italic: boolean;
	underline: boolean;
	strike: boolean;
	inverse: boolean;
}

const reset: Style = {
	fg: null,
	bg: null,
	bold: false,
	dim: false,
	italic: false,
	underline: false,
	strike: false,
	inverse: false
};

function hex(r: number, g: number, b: number): string {
	return `#${[r, g, b].map((n) => n.toString(16).padStart(2, '0')).join('')}`;
}

/** A color of the 256-color palette: the 16 above, a 6×6×6 cube, and 24 grays. */
export function color256(n: number): string | null {
	if (!Number.isInteger(n) || n < 0 || n > 255) {
		return null;
	}
	if (n < 16) {
		return palette[n];
	}
	if (n < 232) {
		const levels = [0, 95, 135, 175, 215, 255];
		const i = n - 16;
		return hex(levels[Math.floor(i / 36)], levels[Math.floor(i / 6) % 6], levels[i % 6]);
	}
	const g = 8 + (n - 232) * 10;
	return hex(g, g, g);
}

function rgb(r: number, g: number, b: number): string | null {
	if (![r, g, b].every(Number.isFinite)) {
		return null;
	}
	const c = (n: number) => Math.min(255, Math.max(0, Math.round(n)));
	return hex(c(r), c(g), c(b));
}

/** The style after an SGR sequence's parameters, such as "1;31" or "38;5;208". */
function applySgr(from: Style, params: string): Style {
	const s = { ...from };
	const parts = params === '' ? ['0'] : params.split(';');
	for (let i = 0; i < parts.length; i++) {
		// Colons separate the parts of one parameter, as in 38:2::255:128:0.
		const sub = parts[i].split(':').map(Number);
		const code = sub[0];
		if (code === 38 || code === 48) {
			let mode: number;
			let args: number[];
			if (sub.length > 1) {
				mode = sub[1];
				args = sub.slice(2);
				if (mode === 2 && args.length > 3) {
					// A color space ID comes first.
					args = args.slice(1);
				}
			} else {
				mode = Number(parts[i + 1]);
				const count = mode === 5 ? 1 : mode === 2 ? 3 : 0;
				args = parts.slice(i + 2, i + 2 + count).map(Number);
				if (count === 0) {
					// A color mode that isn't known: the rest can't be read.
					break;
				}
				i += 1 + count;
			}
			const color =
				mode === 5 ? color256(args[0]) : mode === 2 ? rgb(args[0], args[1], args[2]) : null;
			if (color) {
				s[code === 38 ? 'fg' : 'bg'] = color;
			}
			continue;
		}
		if (code >= 30 && code <= 37) {
			s.fg = palette[code - 30];
		} else if (code >= 90 && code <= 97) {
			s.fg = palette[code - 82];
		} else if (code >= 40 && code <= 47) {
			s.bg = palette[code - 40];
		} else if (code >= 100 && code <= 107) {
			s.bg = palette[code - 92];
		} else {
			switch (code) {
				case 0:
					Object.assign(s, reset);
					break;
				case 1:
					s.bold = true;
					break;
				case 2:
					s.dim = true;
					break;
				case 3:
					s.italic = true;
					break;
				case 4:
				case 21:
					// 4:0 is "no underline".
					s.underline = sub[1] !== 0;
					break;
				case 7:
					s.inverse = true;
					break;
				case 9:
					s.strike = true;
					break;
				case 22:
					s.bold = false;
					s.dim = false;
					break;
				case 23:
					s.italic = false;
					break;
				case 24:
					s.underline = false;
					break;
				case 27:
					s.inverse = false;
					break;
				case 29:
					s.strike = false;
					break;
				case 39:
					s.fg = null;
					break;
				case 49:
					s.bg = null;
					break;
			}
		}
	}
	return s;
}

function channels(color: string): [number, number, number] {
	const n = parseInt(color.slice(1), 16);
	return [(n >> 16) & 255, (n >> 8) & 255, n & 255];
}

/** The relative luminance of a #rrggbb color, from 0 to 1. */
function luminance(color: string): number {
	const [r, g, b] = channels(color).map((c) => {
		const v = c / 255;
		return v <= 0.04045 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
	});
	return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function contrast(a: string, b: string): number {
	const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
	return (hi + 0.05) / (lo + 0.05);
}

function mix(from: string, to: string, t: number): string {
	const a = channels(from);
	const b = channels(to);
	return hex(...(a.map((c, i) => Math.round(c + (b[i] - c) * t)) as [number, number, number]));
}

/**
 * A text color that can be read on a background: the color itself if it can
 * be, and otherwise the nearest one that can, lighter on a dark background
 * and darker on a light one. A program that asks for navy text on a black
 * terminal gets what it can see.
 */
function legible(fg: string, bg: string): string {
	if (contrast(fg, bg) >= minContrast) {
		return fg;
	}
	const target = luminance(bg) > 0.4 ? '#000000' : '#ffffff';
	for (const t of [0.2, 0.4, 0.6, 0.8]) {
		const c = mix(fg, target, t);
		if (contrast(c, bg) >= minContrast) {
			return c;
		}
	}
	return target;
}

/** The inline style of a style, or an empty string for the default one. */
function css(s: Style): string {
	let fg = s.fg;
	let bg = s.bg;
	if (s.inverse) {
		[fg, bg] = [bg ?? defaultBg, fg ?? defaultFg];
	}
	if (bg !== null) {
		fg = legible(fg ?? defaultFg, bg);
	} else if (fg !== null) {
		fg = legible(fg, defaultBg);
	}
	const out: string[] = [];
	if (fg !== null) out.push(`color:${fg}`);
	if (bg !== null) out.push(`background-color:${bg}`);
	if (s.bold) out.push('font-weight:600');
	if (s.dim) out.push('opacity:0.65');
	if (s.italic) out.push('font-style:italic');
	if (s.underline || s.strike) {
		out.push(
			`text-decoration:${[s.underline && 'underline', s.strike && 'line-through'].filter(Boolean).join(' ')}`
		);
	}
	return out.join(';');
}

function add(out: AnsiSegment[], text: string, style: string) {
	if (text === '') {
		return;
	}
	const last = out[out.length - 1];
	if (last && last.css === style) {
		last.text += text;
	} else {
		out.push({ text, css: style });
	}
}

/**
 * Cuts a line into runs of text and the style of each. The text of all the
 * runs together is `plainText(input)`. Only colors, bold, dim, italic,
 * underline and strike-through are kept. A color sequence the line ends in
 * the middle of is dropped, not shown.
 */
export function parseAnsi(input: string): AnsiSegment[] {
	const text = afterReturns(input);
	const out: AnsiSegment[] = [];
	let style = reset;
	let styleCss = '';
	let from = 0;
	TOKEN.lastIndex = 0;
	for (let m = TOKEN.exec(text); m; m = TOKEN.exec(text)) {
		add(out, text.slice(from, m.index), styleCss);
		from = m.index + m[0].length;
		if (m[2] === 'm' && /^[0-9;:]*$/.test(m[1])) {
			style = applySgr(style, m[1]);
			styleCss = css(style);
		}
	}
	add(out, text.slice(from), styleCss);
	return out;
}
