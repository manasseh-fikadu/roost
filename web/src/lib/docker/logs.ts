// The logic of the log viewer that doesn't need a browser: the lines it
// keeps, searching them, picking up a stream where it left off, and which
// rows to draw.

import type { DockerLogLine } from '#lib/api/types.js';
import { plainText, type AnsiSegment } from './ansi.js';

/** How many lines the viewer keeps. The oldest go first. */
export const maxLines = 10_000;
/** The most characters of a line the viewer keeps. The server cuts lines at 64 KB. */
export const maxLineChars = 8_192;

/** A line of the log, ready to show. */
export interface LogEntry {
	/** Counts the lines as they arrive, from 1, so it names a line for as long as it is shown. */
	n: number;
	/** 1 for standard output and 2 for standard error. */
	s: 1 | 2;
	/** The time Docker stamped the line with, as it wrote it. */
	t: string | null;
	/** The text as the container wrote it, with its escape codes. */
	m: string;
	/** The text without escape codes, which is what search reads. */
	p: string;
	/** How many characters were cut off the end of a very long line. */
	cut: number;
}

export function makeEntry(n: number, line: DockerLogLine): LogEntry {
	let m = line.m;
	let cut = 0;
	if (m.length > maxLineChars) {
		let end = maxLineChars;
		// Don't leave half of a surrogate pair.
		const last = m.charCodeAt(end - 1);
		if (last >= 0xd800 && last <= 0xdbff) {
			end--;
		}
		cut = m.length - end;
		m = m.slice(0, end);
	}
	return { n, s: line.s === 2 ? 2 : 1, t: line.t || null, m, p: plainText(m), cut };
}

/** The lines after adding a batch, without the oldest if there are more than `max`. */
export function appendLines(
	lines: LogEntry[],
	batch: LogEntry[],
	max: number = maxLines
): LogEntry[] {
	if (batch.length === 0) {
		return lines;
	}
	const all = lines.length === 0 ? batch.slice() : lines.concat(batch);
	return all.length > max ? all.slice(all.length - max) : all;
}

// ---- Search ----

/** The start and end of a match in a line's plain text. */
export type Range = [start: number, end: number];

/** A search, ready to run. */
export interface Search {
	/** Whether a line matches, or null if there's nothing to search for. */
	test: ((plain: string) => boolean) | null;
	/** Where a line matches, or null if there's nothing to search for. */
	ranges: ((plain: string) => Range[]) | null;
	/** Why the query can't be used, or null. */
	error: string | null;
}

export const noSearch: Search = { test: null, ranges: null, error: null };

/** The most matches of one line that are marked. */
const maxMarks = 100;

/**
 * How much of a line a regular expression is run on. A pattern that backtracks
 * badly takes time that grows with the line, and the viewer cuts lines at 8,192
 * characters anyway.
 */
const maxRegexText = 2_048;

export function escapeRegExp(s: string): string {
	return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

/**
 * A search for a text, or for a regular expression, ignoring case. A
 * regular expression that doesn't parse finds nothing to filter by, and says
 * so in `error`.
 */
export function compileSearch(query: string, regex: boolean): Search {
	const q = regex ? query : query.trim();
	if (q === '') {
		return noSearch;
	}
	const source = regex ? q : escapeRegExp(q);
	let finder: RegExp;
	let tester: RegExp;
	try {
		finder = new RegExp(source, 'gi');
		tester = new RegExp(source, 'i');
	} catch {
		return { test: null, ranges: null, error: 'Not a valid regular expression' };
	}
	const limit = (plain: string) =>
		regex && plain.length > maxRegexText ? plain.slice(0, maxRegexText) : plain;
	return {
		error: null,
		test: (plain) => tester.test(limit(plain)),
		ranges: (full) => {
			const plain = limit(full);
			const out: Range[] = [];
			finder.lastIndex = 0;
			for (let m = finder.exec(plain); m && out.length < maxMarks; m = finder.exec(plain)) {
				if (m[0] === '') {
					// A pattern that matches nothing, such as a*, would never move on.
					finder.lastIndex++;
					continue;
				}
				out.push([m.index, m.index + m[0].length]);
			}
			return out;
		}
	};
}

/** The lines that match a search, or all of them without one. */
export function filterLines(lines: LogEntry[], search: Search): LogEntry[] {
	const test = search.test;
	return test ? lines.filter((e) => test(e.p)) : lines;
}

/** A piece of a line: some text, its style, and whether a search matched it. */
export interface Piece {
	text: string;
	css: string;
	mark: boolean;
}

/**
 * Cuts the segments of a line at the edges of the matches, so the matched
 * text can be marked. `ranges` are in order and don't overlap, and count
 * characters of the segments' text together.
 */
export function splitMarks(segments: AnsiSegment[], ranges: Range[]): Piece[] {
	const out: Piece[] = [];
	let pos = 0;
	let r = 0;
	for (const seg of segments) {
		const end = pos + seg.text.length;
		let at = pos;
		while (r < ranges.length && ranges[r][0] < end) {
			const [start, stop] = ranges[r];
			const from = Math.max(start, at);
			if (from > at) {
				out.push({ text: seg.text.slice(at - pos, from - pos), css: seg.css, mark: false });
			}
			const to = Math.min(stop, end);
			if (to > from) {
				out.push({ text: seg.text.slice(from - pos, to - pos), css: seg.css, mark: true });
			}
			at = to;
			if (stop <= end) {
				r++;
			} else {
				// The match goes on in the next segment.
				break;
			}
		}
		if (at < end) {
			out.push({ text: seg.text.slice(at - pos), css: seg.css, mark: false });
		}
		pos = end;
	}
	return out;
}

/** The count under the log: "1,204 lines", or "12 of 1,204 lines" while a search hides some. */
export function lineCount(shown: number, total: number, searching: boolean): string {
	const noun = total === 1 ? 'line' : 'lines';
	return searching
		? `${shown.toLocaleString()} of ${total.toLocaleString()} ${noun}`
		: `${total.toLocaleString()} ${noun}`;
}

// ---- Picking a stream up again ----

const rfc3339 = /^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.(\d+))?Z$/;

/**
 * A time that sorts as text: UTC, with the fraction padded to nanoseconds,
 * since Docker leaves off the zeros at the end of it.
 */
export function timeKey(t: string): string {
	const m = rfc3339.exec(t);
	if (m) {
		return `${m[1]}.${(m[2] ?? '').slice(0, 9).padEnd(9, '0')}Z`;
	}
	const ms = Date.parse(t);
	if (Number.isNaN(ms)) {
		return t;
	}
	const frac = String(((ms % 1000) + 1000) % 1000).padStart(3, '0');
	return `${new Date(ms).toISOString().slice(0, 19)}.${frac}000000Z`;
}

/**
 * Where to ask for the logs again after the connection broke, given the
 * time of the newest line there is. Docker starts at a whole second, and
 * output and error lines can come a little out of order, so this starts two
 * seconds before: `since` is the time to ask for, and `key` sorts the same
 * as the times of lines. It returns null if the time makes no sense.
 */
export function resumePoint(newest: string): { since: string; key: string } | null {
	const ms = Date.parse(newest);
	if (Number.isNaN(ms)) {
		return null;
	}
	const since = new Date((Math.floor(ms / 1000) - 2) * 1000).toISOString().replace('.000Z', 'Z');
	return { since, key: timeKey(since) };
}

/** What identifies a line when a stream sends it a second time. */
function repeatKey(s: number, t: string, length: number, start: string): string {
	return `${s}|${t}|${length}|${start}`;
}

/** The lines that are shown from `key` on, counted, to drop when a resumed stream sends them again. */
export function recentCounts(lines: readonly LogEntry[], key: string): Map<string, number> {
	const seen = new Map<string, number>();
	for (let i = lines.length - 1; i >= 0; i--) {
		const e = lines[i];
		if (e.t && timeKey(e.t) >= key) {
			const k = repeatKey(e.s, e.t, e.m.length + e.cut, e.m.slice(0, 200));
			seen.set(k, (seen.get(k) ?? 0) + 1);
		}
	}
	return seen;
}

/** Whether a line was shown already. If it was, it uses up one of its count. */
export function takeRepeat(seen: Map<string, number>, line: DockerLogLine): boolean {
	if (!line.t) {
		return false;
	}
	const k = repeatKey(line.s, line.t, line.m.length, line.m.slice(0, 200));
	const n = seen.get(k) ?? 0;
	if (n === 0) {
		return false;
	}
	seen.set(k, n - 1);
	return true;
}

// ---- Choices ----

export const sinceOptions: { value: string; label: string }[] = [
	{ value: '', label: 'All time' },
	{ value: '15m', label: 'Last 15 minutes' },
	{ value: '1h', label: 'Last hour' },
	{ value: '6h', label: 'Last 6 hours' },
	{ value: '24h', label: 'Last 24 hours' }
];

export const tailOptions = [100, 500, 1000, 5000];
export const defaultTail = 500;

// ---- Times ----

/** A line's time of day in the browser's time zone, such as 14:05:09.007. */
export function logTime(t: string | null): string {
	if (!t) {
		return '';
	}
	const d = new Date(t);
	if (Number.isNaN(d.getTime())) {
		return t;
	}
	const p = (n: number, width = 2) => String(n).padStart(width, '0');
	return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
}

// ---- Which rows to draw ----

/** The height of a row, in pixels, when lines don't wrap. */
export const rowHeight = 18;
/** The rows drawn past each edge of the box, so a quick scroll shows no gap. */
export const overscan = 12;
/** How many lines to draw at most when lines wrap, whose heights can't be known. */
export const wrapWindow = 2_000;

/** The rows to draw when the box is scrolled to `scrollTop` and `viewport` pixels tall. */
export function scrollRange(
	scrollTop: number,
	viewport: number,
	count: number,
	rowH: number = rowHeight
): { start: number; end: number } {
	const first = Math.floor(Math.max(0, scrollTop) / rowH);
	return {
		start: Math.min(count, Math.max(0, first - overscan)),
		end: Math.min(count, first + Math.ceil(viewport / rowH) + overscan)
	};
}

/** The rows to draw when the box shows the end of the log. */
export function tailRange(
	viewport: number,
	count: number,
	rowH: number = rowHeight
): { start: number; end: number } {
	return { start: Math.max(0, count - Math.ceil(viewport / rowH) - overscan), end: count };
}
