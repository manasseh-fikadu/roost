// A small Markdown parser for READMEs and notes. It makes a tree that Markdown.svelte draws as
// elements, never as HTML strings, so a hostile file can't inject markup: raw HTML in the source
// shows up as text, and every link and image goes through a resolver that only lets safe URLs by.
//
// It is hand-written and has no dependencies. It covers headings, paragraphs, emphasis, code,
// links, images, quotes, lists with task boxes, tables and rules, which is what READMEs use.

export type Align = 'left' | 'center' | 'right';

export type Inline =
	| { type: 'text'; text: string }
	| { type: 'code'; text: string }
	/** `src` is null for an image that may not load; it shows as its alt text. */
	| { type: 'image'; src: string | null; alt: string }
	/** `href` is null for a link that may not be followed; it shows as plain link text. */
	| { type: 'link'; href: string | null; children: Inline[] }
	| { type: 'strong' | 'em' | 'del'; children: Inline[] };

export interface ListItem {
	/** Whether the item's task box is checked, or null if it has no box. */
	checked: boolean | null;
	inline: Inline[];
	/** What is nested under the item: more paragraphs, lists, code. */
	blocks: Block[];
}

export type Block =
	| { type: 'paragraph'; inline: Inline[] }
	| { type: 'heading'; level: 1 | 2 | 3 | 4 | 5 | 6; inline: Inline[] }
	| { type: 'code'; text: string; lang: string }
	| { type: 'rule' }
	| { type: 'quote'; blocks: Block[] }
	| { type: 'table'; align: Align[]; head: Inline[][]; rows: Inline[][][] }
	| { type: 'list'; ordered: boolean; start: number; items: ListItem[] };

/** Turns the target of a link or image into a URL that is safe to use, or null to refuse it. */
export type Resolve = (url: string, kind: 'link' | 'image') => string | null;

const SAFE_URL = /^(https?:|mailto:)/i;
const HAS_SCHEME = /^[a-z][\w+.-]*:/i;
// The URL parser ignores control characters, so "\x01javascript:" is a script link to a browser.
const CONTROL = /[\u0000-\u001f\u007f]/;

/**
 * The policy for a document's links and images. Absolute http, https and mailto links are kept,
 * and so are https images, which load from their own server as the page opens. Everything else
 * with a scheme (javascript:, data:, file:), a "#anchor" or a "//host" is refused. A relative
 * target goes to `relative`, which must answer with a path on this origin, or null.
 */
export function markdownResolver(relative: (url: string) => string | null): Resolve {
	return (url, kind) => {
		if (CONTROL.test(url)) {
			return null;
		}
		if (SAFE_URL.test(url)) {
			return kind === 'image' && !/^https:/i.test(url) ? null : url;
		}
		if (HAS_SCHEME.test(url) || url.startsWith('#') || url.startsWith('//')) {
			return null;
		}
		const out = relative(url);
		// A path on this origin only: not "//host", "/\host" or a URL with a scheme.
		return out && /^\/(?![/\\])/.test(out) ? out : null;
	};
}

/**
 * "/docs" and "../img/a.png" make "/img/a.png": where a relative link in a document points on the
 * same machine. A query or fragment is dropped. It returns null for a target that isn't a path.
 */
export function resolveRelative(dir: string, rel: string): string | null {
	try {
		const segments = dir.split('/').filter(Boolean).map(encodeURIComponent);
		const base = `file:///${segments.map((s) => `${s}/`).join('')}`;
		return decodeURIComponent(new URL(rel.split(/[?#]/)[0], base).pathname);
	} catch {
		return null;
	}
}

/** How far ahead to look for the mark that closes a span. */
const REACH = 1000;
/**
 * How much searching for closing marks a run of text may do, per character of it. Honest text
 * uses a few steps a character. A file of nothing but opening marks would use REACH, and take
 * minutes, so once the run has spent its budget its remaining marks are plain text.
 */
const BUDGET_PER_CHAR = 8;
/** How deep quotes, lists and links may nest. Deeper than this is shown as plain text. */
const MAX_DEPTH = 10;

interface Context {
	resolve: Resolve;
	/** Steps left for searching. */
	budget: number;
}

/** Spends steps, and says whether there were enough. */
function spend(ctx: Context, steps: number): boolean {
	ctx.budget -= steps;
	return ctx.budget > 0;
}

const PLAIN = /[^\\`![<*_~]+/y;
const ESCAPABLE = /[!-/:-@[-`{-~]/;
const AUTOLINK = /<(https?:\/\/[^>\s]{1,2000})>/y;
const DEST = /\(\s*<?([^)\s>]{1,2000})>?(?:\s+"[^"]{0,500}")?\s*\)/y;
const WORD = /\w/;
const SPACE = /\s/;

/** Where the span that opens at `from` and holds ticks of length `n` closes, or -1. */
function closeTicks(text: string, from: number, n: number, ctx: Context): number {
	const stop = Math.min(text.length, from + REACH);
	let j = from;
	while (j < stop && spend(ctx, 1)) {
		const k = text.indexOf('`', j);
		if (k < 0 || k >= stop) {
			return -1;
		}
		let run = 1;
		while (text[k + run] === '`') {
			run++;
		}
		if (run === n) {
			return k;
		}
		j = k + run;
	}
	return -1;
}

/** The "]" that closes the "[" at `open`, counting nested brackets, or -1. */
function closeBracket(text: string, open: number, ctx: Context): number {
	const stop = Math.min(text.length, open + REACH);
	let depth = 0;
	for (let j = open; j < stop; j++) {
		const c = text[j];
		if (c === '\\') {
			j++;
		} else if (c === '[') {
			depth++;
		} else if (c === ']' && --depth === 0) {
			spend(ctx, j - open);
			return j;
		}
	}
	spend(ctx, stop - open);
	return -1;
}

/** A link or image whose "[" is at `open`, or null if the text there isn't one. */
function linkAt(
	text: string,
	open: number,
	image: boolean,
	ctx: Context,
	depth: number
): { node: Inline; end: number } | null {
	if (ctx.budget <= 0) {
		return null;
	}
	const close = closeBracket(text, open, ctx);
	if (close < 0 || close === open + 1 || text[close + 1] !== '(') {
		return null;
	}
	// Matching a target costs more when it fails, as it tries every length.
	spend(ctx, 50);
	DEST.lastIndex = close + 1;
	const dest = DEST.exec(text);
	if (!dest) {
		spend(ctx, 450);
		return null;
	}
	const label = text.slice(open + 1, close);
	const end = close + 1 + dest[0].length;
	if (image) {
		return { node: { type: 'image', src: ctx.resolve(dest[1], 'image'), alt: label }, end };
	}
	const children: Inline[] =
		depth < MAX_DEPTH ? inlineIn(label, ctx, depth + 1) : [{ type: 'text', text: label }];
	return { node: { type: 'link', href: ctx.resolve(dest[1], 'link'), children }, end };
}

/**
 * Emphasis, strong or strikethrough that opens at `i`: `*a*`, `_a_`, `**a**`, `__a__` or `~~a~~`.
 * The opening mark has to touch its text, and the closing one too, so "2 ** 3 ** 4" stays as it
 * is. An underscore inside a word, as in snake_case_name, isn't a mark.
 */
function emphasisAt(
	text: string,
	i: number,
	c: string,
	ctx: Context
): { kind: 'strong' | 'em' | 'del'; inner: string; end: number } | null {
	const double = text[i + 1] === c;
	if (c === '~' && !double) {
		return null;
	}
	if (c === '_' && WORD.test(text[i - 1] ?? '')) {
		return null;
	}
	const mark = double ? c + c : c;
	const from = i + mark.length;
	if (from >= text.length || SPACE.test(text[from]) || ctx.budget <= 0) {
		return null;
	}
	const stop = Math.min(text.length, from + REACH);
	for (
		let j = text.indexOf(mark, from + 1);
		j >= 0 && j < stop && spend(ctx, 1);
		j = text.indexOf(mark, j + 1)
	) {
		const before = text[j - 1];
		const after = text[j + mark.length] ?? '';
		if (before === '\\' || SPACE.test(before)) {
			continue;
		}
		// A single mark doesn't close on half of a double one.
		if (!double && (before === c || after === c)) {
			continue;
		}
		if (c === '_' && WORD.test(after)) {
			continue;
		}
		const kind = c === '~' ? 'del' : double ? 'strong' : 'em';
		return { kind, inner: text.slice(from, j), end: j + mark.length };
	}
	return null;
}

/** The inline parts of a run of text: emphasis, code, links and images. Everything else is text. */
export function parseInline(text: string, resolve: Resolve): Inline[] {
	return inlineIn(text, { resolve, budget: BUDGET_PER_CHAR * text.length + 1000 }, 0);
}

function inlineIn(text: string, ctx: Context, depth: number): Inline[] {
	const out: Inline[] = [];
	let plain = '';
	const push = (node: Inline) => {
		if (plain) {
			out.push({ type: 'text', text: plain });
			plain = '';
		}
		out.push(node);
	};
	let i = 0;
	while (i < text.length) {
		PLAIN.lastIndex = i;
		const run = PLAIN.exec(text);
		if (run) {
			plain += run[0];
			i += run[0].length;
			continue;
		}
		const c = text[i];
		if (c === '\\' && i + 1 < text.length && ESCAPABLE.test(text[i + 1])) {
			plain += text[i + 1];
			i += 2;
			continue;
		}
		if (c === '`') {
			let n = 1;
			while (text[i + n] === '`') {
				n++;
			}
			const end = closeTicks(text, i + n, n, ctx);
			const code = end < 0 ? '' : text.slice(i + n, end).trim();
			if (code) {
				push({ type: 'code', text: code });
				i = end + n;
			} else {
				plain += '`'.repeat(n);
				i += n;
			}
			continue;
		}
		if (c === '[' || (c === '!' && text[i + 1] === '[')) {
			const image = c === '!';
			const hit = linkAt(text, image ? i + 1 : i, image, ctx, depth);
			if (hit) {
				push(hit.node);
				i = hit.end;
				continue;
			}
		}
		if (c === '<' && text.startsWith('<http', i) && spend(ctx, 50)) {
			AUTOLINK.lastIndex = i;
			const m = AUTOLINK.exec(text);
			if (!m) {
				spend(ctx, 450);
			} else {
				push({
					type: 'link',
					href: ctx.resolve(m[1], 'link'),
					children: [{ type: 'text', text: m[1] }]
				});
				i += m[0].length;
				continue;
			}
		}
		if (c === '*' || c === '_' || c === '~') {
			const hit = emphasisAt(text, i, c, ctx);
			if (hit) {
				push({
					type: hit.kind,
					children:
						depth < MAX_DEPTH
							? inlineIn(hit.inner, ctx, depth + 1)
							: [{ type: 'text', text: hit.inner }]
				});
				i = hit.end;
				continue;
			}
		}
		plain += c;
		i++;
	}
	if (plain) {
		out.push({ type: 'text', text: plain });
	}
	return out;
}

const FENCE = /^\s{0,3}(`{3,}|~{3,})(.*)$/;
const FENCE_END: Record<string, RegExp> = {
	'`': /^\s{0,3}`{3,}\s*$/,
	'~': /^\s{0,3}~{3,}\s*$/
};
const HEADING = /^\s{0,3}(#{1,6})\s+(.*)$/;
const SETEXT = /^\s{0,3}(=+|-{2,})\s*$/;
const RULE = /^\s{0,3}([-*_])(\s*\1){2,}\s*$/;
const QUOTE = /^\s{0,3}>/;
const LIST = /^\s{0,3}([-*+]|\d{1,9}[.)])\s+/;
const MARKER = /^\s*([-*+]|\d{1,9}[.)])\s+/;
const TASK = /^\[([ xX])\]\s+/;
const TABLE_SEPARATOR = /^:?-+:?$/;

interface Fence {
	mark: string;
	lang: string;
}

function fenceOf(line: string): Fence | null {
	const m = FENCE.exec(line);
	// A backtick fence can't have backticks in its info, or "```code``` text" would open one.
	if (!m || (m[1][0] === '`' && m[2].includes('`'))) {
		return null;
	}
	return { mark: m[1], lang: m[2].trim().split(/\s+/)[0] };
}

function closesFence(line: string, fence: Fence): boolean {
	return FENCE_END[fence.mark[0]].test(line) && line.trim().length >= fence.mark.length;
}

/** A heading's text without its optional closing hashes: "Title ##" is "Title", but "C#" is "C#". */
function headingText(raw: string): string {
	const s = raw.trimEnd();
	let end = s.length;
	while (end > 0 && s[end - 1] === '#') {
		end--;
	}
	return end < s.length && (end === 0 || SPACE.test(s[end - 1])) ? s.slice(0, end).trimEnd() : s;
}

/** The cells of a table row, without its edge pipes. A "\|" stays in its cell. */
function cellsOf(line: string): string[] {
	let s = line.trim();
	if (s.startsWith('|')) {
		s = s.slice(1);
	}
	if (s.endsWith('|') && !s.endsWith('\\|')) {
		s = s.slice(0, -1);
	}
	return s.split(/(?<!\\)\|/).map((c) => c.trim());
}

function isTableSeparator(line: string): boolean {
	return line.includes('|') && cellsOf(line).every((c) => TABLE_SEPARATOR.test(c));
}

const isOrdered = (marker: string) => /\d/.test(marker[0]);

function indentOf(line: string): number {
	return line.length - line.trimStart().length;
}

function parseBlocks(lines: string[], resolve: Resolve, depth: number): Block[] {
	if (depth > MAX_DEPTH) {
		const text = lines.join('\n').trim();
		return text ? [{ type: 'paragraph', inline: [{ type: 'text', text }] }] : [];
	}
	const out: Block[] = [];
	const inline = (s: string) => parseInline(s, resolve);
	let i = 0;
	while (i < lines.length) {
		const line = lines[i];
		if (!line.trim()) {
			i++;
			continue;
		}

		const fence = fenceOf(line);
		if (fence) {
			const body: string[] = [];
			i++;
			while (i < lines.length && !closesFence(lines[i], fence)) {
				body.push(lines[i++]);
			}
			i++;
			out.push({ type: 'code', text: body.join('\n'), lang: fence.lang });
			continue;
		}

		const heading = HEADING.exec(line);
		if (heading) {
			out.push({
				type: 'heading',
				level: heading[1].length as 1 | 2 | 3 | 4 | 5 | 6,
				inline: inline(headingText(heading[2]))
			});
			i++;
			continue;
		}

		if (RULE.test(line)) {
			out.push({ type: 'rule' });
			i++;
			continue;
		}

		if (QUOTE.test(line)) {
			const body: string[] = [];
			while (i < lines.length && lines[i].trim() && QUOTE.test(lines[i])) {
				body.push(lines[i++].replace(/^\s{0,3}>\s?/, ''));
			}
			out.push({ type: 'quote', blocks: parseBlocks(body, resolve, depth + 1) });
			continue;
		}

		if (line.includes('|') && i + 1 < lines.length && isTableSeparator(lines[i + 1])) {
			const head = cellsOf(line);
			const separator = cellsOf(lines[i + 1]);
			if (head.length === separator.length) {
				const align = separator.map((c): Align => {
					return c.endsWith(':') ? (c.startsWith(':') ? 'center' : 'right') : 'left';
				});
				const rows: string[][] = [];
				i += 2;
				while (i < lines.length && lines[i].trim() && lines[i].includes('|')) {
					rows.push(cellsOf(lines[i++]));
				}
				out.push({
					type: 'table',
					align,
					head: head.map(inline),
					rows: rows.map((row) => head.map((_, j) => inline(row[j] ?? '')))
				});
				continue;
			}
		}

		if (LIST.test(line)) {
			const first = MARKER.exec(line)!;
			const ordered = isOrdered(first[1]);
			const indent = indentOf(line);
			// Each item gathers its lines, with the indent of its text taken off.
			const items: Array<{ lines: string[]; width: number }> = [];
			while (i < lines.length) {
				const l = lines[i];
				const lead = indentOf(l);
				const last = items[items.length - 1];
				const marker = MARKER.exec(l);
				// A bullet after numbers, or numbers after a bullet, starts another list.
				if (l.trim() && lead <= indent + 1 && LIST.test(l) && isOrdered(marker![1]) === ordered) {
					items.push({ lines: [l.replace(MARKER, '')], width: marker![0].length });
				} else if (l.trim() && lead > indent) {
					last?.lines.push(l.slice(Math.min(lead, last.width)));
				} else if (
					!l.trim() &&
					i + 1 < lines.length &&
					(indentOf(lines[i + 1]) > indent || LIST.test(lines[i + 1]))
				) {
					last?.lines.push('');
				} else {
					break;
				}
				i++;
			}
			out.push({
				type: 'list',
				ordered,
				start: ordered ? Number.parseInt(first[1], 10) : 1,
				items: items.map(({ lines: item }): ListItem => {
					const task = TASK.exec(item[0]);
					return {
						checked: task ? task[1] !== ' ' : null,
						inline: inline(task ? item[0].slice(task[0].length) : item[0]),
						blocks: parseBlocks(item.slice(1), resolve, depth + 1)
					};
				})
			});
			continue;
		}

		// A paragraph runs to a blank line or the start of another block. Text over a line of "=" or
		// "-" is a heading instead.
		const para: string[] = [];
		let setext = 0;
		while (
			i < lines.length &&
			lines[i].trim() &&
			!fenceOf(lines[i]) &&
			!HEADING.test(lines[i]) &&
			!QUOTE.test(lines[i]) &&
			!(para.length && LIST.test(lines[i]))
		) {
			if (para.length) {
				const under = SETEXT.exec(lines[i]);
				if (under) {
					setext = under[1][0] === '=' ? 1 : 2;
					i++;
					break;
				}
				if (RULE.test(lines[i])) {
					break;
				}
			}
			para.push(lines[i++]);
		}
		if (!para.length) {
			para.push(lines[i++]);
		}
		out.push(
			setext
				? {
						type: 'heading',
						level: setext as 1 | 2,
						inline: inline(para.map((l) => l.trim()).join('\n'))
					}
				: { type: 'paragraph', inline: inline(para.map((l) => l.trim()).join('\n')) }
		);
	}
	return out;
}

/** Parses a document. `resolve` decides which links and images are safe to use. */
export function parseMarkdown(text: string, resolve: Resolve): Block[] {
	return parseBlocks(text.replace(/^﻿/, '').replace(/\r\n?/g, '\n').split('\n'), resolve, 0);
}
