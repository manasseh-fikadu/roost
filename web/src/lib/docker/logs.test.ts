import { describe, expect, test } from 'bun:test';
import type { DockerLogLine } from '#lib/api/types.js';
import { parseAnsi } from './ansi.js';
import {
	appendLines,
	compileSearch,
	filterLines,
	lineCount,
	logTime,
	makeEntry,
	maxLineChars,
	noSearch,
	overscan,
	recentCounts,
	resumePoint,
	scrollRange,
	splitMarks,
	tailRange,
	takeRepeat,
	timeKey,
	type LogEntry
} from './logs.js';

function line(m: string, over: Partial<DockerLogLine> = {}): DockerLogLine {
	return { s: 1, m, ...over };
}

function entries(...texts: string[]): LogEntry[] {
	return texts.map((m, i) => makeEntry(i + 1, line(m)));
}

describe('makeEntry', () => {
	test('keeps plain text as it is', () => {
		const e = makeEntry(7, line('hello', { t: '2026-10-30T12:00:00.5Z' }));
		expect(e).toEqual({ n: 7, s: 1, t: '2026-10-30T12:00:00.5Z', m: 'hello', p: 'hello', cut: 0 });
	});

	test('keeps the text with its escape codes, and searches the text without them', () => {
		const e = makeEntry(1, line('\x1b[31merror\x1b[0m: bad'));
		expect(e.m).toBe('\x1b[31merror\x1b[0m: bad');
		expect(e.p).toBe('error: bad');
	});

	test('standard error is 2 and anything else is 1', () => {
		expect(makeEntry(1, line('x', { s: 2 })).s).toBe(2);
		expect(makeEntry(1, line('x', { s: 3 })).s).toBe(1);
		expect(makeEntry(1, line('x', { s: 0 })).s).toBe(1);
	});

	test('a line with no time has none', () => {
		expect(makeEntry(1, line('x')).t).toBeNull();
		expect(makeEntry(1, line('x', { t: '' })).t).toBeNull();
	});

	test('cuts a very long line and says how much', () => {
		const e = makeEntry(1, line('a'.repeat(maxLineChars + 500)));
		expect(e.m.length).toBe(maxLineChars);
		expect(e.p.length).toBe(maxLineChars);
		expect(e.cut).toBe(500);
	});

	test('does not cut a surrogate pair in half', () => {
		const e = makeEntry(1, line('a'.repeat(maxLineChars - 1) + '😀tail'));
		expect(e.m.length).toBe(maxLineChars - 1);
		expect(e.cut).toBe(6);
	});

	test('a line of exactly the limit is whole', () => {
		const e = makeEntry(1, line('a'.repeat(maxLineChars)));
		expect(e.cut).toBe(0);
	});
});

describe('appendLines', () => {
	const ids = (l: LogEntry[]) => l.map((e) => e.n);

	test('adds a batch to the end', () => {
		const a = entries('1', '2');
		const b = [makeEntry(3, line('3'))];
		expect(ids(appendLines(a, b, 10))).toEqual([1, 2, 3]);
	});

	test('does not change the lines it was given', () => {
		const a = entries('1', '2');
		const out = appendLines(a, [makeEntry(3, line('3'))], 10);
		expect(a.length).toBe(2);
		expect(out).not.toBe(a);
	});

	test('an empty batch gives back the same array, so nothing redraws', () => {
		const a = entries('1');
		expect(appendLines(a, [], 10)).toBe(a);
	});

	test('drops the oldest lines past the limit', () => {
		const a = entries('1', '2', '3', '4');
		const b = [makeEntry(5, line('5')), makeEntry(6, line('6'))];
		expect(ids(appendLines(a, b, 4))).toEqual([3, 4, 5, 6]);
	});

	test('a batch bigger than the limit leaves only its end', () => {
		const a = entries('1', '2');
		const b = entries('a', 'b', 'c', 'd', 'e').map((e, i) => ({ ...e, n: 10 + i }));
		expect(ids(appendLines(a, b, 3))).toEqual([12, 13, 14]);
	});

	test('keeps exactly the limit', () => {
		const a = entries('1', '2', '3');
		expect(appendLines(a, [makeEntry(4, line('4'))], 4).length).toBe(4);
	});

	test('starts from nothing', () => {
		expect(ids(appendLines([], entries('1', '2'), 5))).toEqual([1, 2]);
	});
});

describe('compileSearch', () => {
	test('no query is no search', () => {
		expect(compileSearch('', false)).toBe(noSearch);
		expect(compileSearch('   ', false)).toBe(noSearch);
		expect(compileSearch('', true)).toBe(noSearch);
	});

	const matches: { name: string; query: string; regex: boolean; text: string; want: boolean }[] = [
		{ name: 'a substring', query: 'err', regex: false, text: 'an error', want: true },
		{ name: 'ignores case', query: 'ERR', regex: false, text: 'an error', want: true },
		{
			name: 'ignores case the other way',
			query: 'err',
			regex: false,
			text: 'AN ERROR',
			want: true
		},
		{ name: 'not there', query: 'warn', regex: false, text: 'an error', want: false },
		{ name: 'trims the query', query: '  err ', regex: false, text: 'an error', want: true },
		{ name: 'keeps spaces inside', query: 'an e', regex: false, text: 'an error', want: true },
		{
			name: 'treats regex characters as text',
			query: 'a.c',
			regex: false,
			text: 'abc',
			want: false
		},
		{ name: 'finds a dot as a dot', query: 'a.c', regex: false, text: 'a.c', want: true },
		{ name: 'finds brackets as text', query: '[x]', regex: false, text: 'got [x] ok', want: true },
		{ name: 'finds a backslash', query: 'a\\b', regex: false, text: 'a\\b', want: true },
		{ name: 'a regex', query: 'err(or)?\\b', regex: true, text: 'an error!', want: true },
		{ name: 'a regex is a regex', query: 'a.c', regex: true, text: 'abc', want: true },
		{ name: 'a regex ignores case', query: '^GET', regex: true, text: 'get /', want: true },
		{ name: 'a regex that is not there', query: '^GET', regex: true, text: 'a GET', want: false },
		{ name: 'alternatives', query: 'warn|fatal', regex: true, text: 'fatal: x', want: true },
		// A regex runs on the start of a line, so a pattern that backtracks badly
		// can't take time that grows with a line of any length. A text search
		// has no such limit.
		{
			name: 'a regex stops at 2,048 characters',
			query: 'needle',
			regex: true,
			text: 'x'.repeat(3_000) + 'needle',
			want: false
		},
		{
			name: 'a regex finds what is within 2,048 characters',
			query: 'needle',
			regex: true,
			text: 'x'.repeat(2_000) + 'needle',
			want: true
		},
		{
			name: 'a text search reads the whole line',
			query: 'needle',
			regex: false,
			text: 'x'.repeat(3_000) + 'needle',
			want: true
		}
	];
	for (const c of matches) {
		test(c.name, () => {
			const s = compileSearch(c.query, c.regex);
			expect(s.error).toBeNull();
			expect(s.test?.(c.text)).toBe(c.want);
		});
	}

	test('a regex that does not parse says so and does not throw', () => {
		for (const q of ['(', '[a-', '*a', 'a{2,1}', '\\']) {
			const s = compileSearch(q, true);
			expect(s.error).toBe('Not a valid regular expression');
			expect(s.test).toBeNull();
			expect(s.ranges).toBeNull();
		}
	});

	test('the same text is fine as a plain search', () => {
		const s = compileSearch('(', false);
		expect(s.error).toBeNull();
		expect(s.test?.('f(x)')).toBe(true);
	});

	test('finds every match', () => {
		const s = compileSearch('ab', false);
		expect(s.ranges?.('xabyABzab')).toEqual([
			[1, 3],
			[4, 6],
			[7, 9]
		]);
	});

	test('can run again on another line', () => {
		const s = compileSearch('a', false);
		expect(s.ranges?.('aa')).toEqual([
			[0, 1],
			[1, 2]
		]);
		expect(s.ranges?.('ba')).toEqual([[1, 2]]);
		expect(s.test?.('a')).toBe(true);
		expect(s.test?.('a')).toBe(true);
	});

	test('a pattern that matches nothing at all marks nothing and ends', () => {
		const s = compileSearch('x*', true);
		expect(s.test?.('abc')).toBe(true);
		expect(s.ranges?.('abc')).toEqual([]);
		expect(s.ranges?.('axxc')).toEqual([[1, 3]]);
	});

	test('marks only so many matches in one line', () => {
		const s = compileSearch('a', false);
		expect(s.ranges?.('a'.repeat(500)).length).toBe(100);
	});
});

describe('filterLines', () => {
	const lines = entries('Error: one', 'ok', '\x1b[31mERROR\x1b[0m two', 'fine');

	test('without a search keeps every line, as the same array', () => {
		expect(filterLines(lines, noSearch)).toBe(lines);
	});

	test('keeps the lines that match the text without its escape codes', () => {
		const out = filterLines(lines, compileSearch('error', false));
		expect(out.map((e) => e.n)).toEqual([1, 3]);
	});

	test('a bad regex hides nothing', () => {
		expect(filterLines(lines, compileSearch('(', true))).toBe(lines);
	});
});

describe('splitMarks', () => {
	const pieces = (input: string, query: string) => {
		const e = makeEntry(1, line(input));
		return splitMarks(parseAnsi(e.m), compileSearch(query, false).ranges!(e.p));
	};

	test('splits plain text around the matches', () => {
		expect(pieces('an error here', 'error')).toEqual([
			{ text: 'an ', css: '', mark: false },
			{ text: 'error', css: '', mark: true },
			{ text: ' here', css: '', mark: false }
		]);
	});

	test('marks a match at the start and at the end', () => {
		expect(pieces('ab', 'a')).toEqual([
			{ text: 'a', css: '', mark: true },
			{ text: 'b', css: '', mark: false }
		]);
		expect(pieces('ab', 'b')).toEqual([
			{ text: 'a', css: '', mark: false },
			{ text: 'b', css: '', mark: true }
		]);
	});

	test('marks several matches', () => {
		expect(pieces('abab', 'b').map((p) => [p.text, p.mark])).toEqual([
			['a', false],
			['b', true],
			['a', false],
			['b', true]
		]);
	});

	test('keeps each segment its style', () => {
		const out = pieces('\x1b[31mred err\x1b[0m ok err', 'err');
		expect(out.map((p) => [p.text, p.css !== '', p.mark])).toEqual([
			['red ', true, false],
			['err', true, true],
			[' ok ', false, false],
			['err', false, true]
		]);
	});

	test('a match that crosses a change of style is marked in both parts', () => {
		const out = pieces('ab\x1b[31mcd\x1b[0mef', 'bcde');
		expect(out.map((p) => [p.text, p.css !== '', p.mark])).toEqual([
			['a', false, false],
			['b', false, true],
			['cd', true, true],
			['e', false, true],
			['f', false, false]
		]);
	});

	test('the text of the pieces is the text of the segments', () => {
		const input = '\x1b[1mfoo\x1b[0m bar \x1b[32mfoo\x1b[0m';
		expect(
			pieces(input, 'o b')
				.map((p) => p.text)
				.join('')
		).toBe('foo bar foo');
	});

	test('no matches leaves the segments', () => {
		expect(splitMarks(parseAnsi('a\x1b[31mb'), [])).toEqual([
			{ text: 'a', css: '', mark: false },
			{ text: 'b', css: expect.any(String), mark: false }
		]);
	});
});

describe('lineCount', () => {
	const cases: [number, number, boolean, string][] = [
		[5, 5, false, '5 lines'],
		[1, 1, false, '1 line'],
		[0, 0, false, '0 lines'],
		[2, 10, true, '2 of 10 lines'],
		[0, 10, true, '0 of 10 lines'],
		[1, 1, true, '1 of 1 line']
	];
	for (const [shown, total, searching, want] of cases) {
		test(want, () => {
			expect(lineCount(shown, total, searching)).toBe(want);
		});
	}
});

describe('timeKey', () => {
	const cases: [string, string][] = [
		['2026-10-30T12:00:00Z', '2026-10-30T12:00:00.000000000Z'],
		['2026-10-30T12:00:00.5Z', '2026-10-30T12:00:00.500000000Z'],
		['2026-10-30T12:00:00.123456789Z', '2026-10-30T12:00:00.123456789Z'],
		['2026-10-30T12:00:00.1234567891Z', '2026-10-30T12:00:00.123456789Z'],
		['2026-10-30T14:00:00.25+02:00', '2026-10-30T12:00:00.250000000Z'],
		['not a time', 'not a time']
	];
	for (const [t, want] of cases) {
		test(t, () => {
			expect(timeKey(t)).toBe(want);
		});
	}

	test('keys sort like the times they stand for', () => {
		const times = ['2026-10-30T12:00:00.5Z', '2026-10-30T12:00:00.25Z', '2026-10-30T12:00:00Z'];
		expect(times.map(timeKey).sort()).toEqual(
			['2026-10-30T12:00:00Z', '2026-10-30T12:00:00.25Z', '2026-10-30T12:00:00.5Z'].map(timeKey)
		);
	});
});

describe('resumePoint', () => {
	test('starts two whole seconds before the newest line', () => {
		expect(resumePoint('2026-10-30T12:00:07.987654321Z')).toEqual({
			since: '2026-10-30T12:00:05Z',
			key: '2026-10-30T12:00:05.000000000Z'
		});
	});

	test('crosses a minute', () => {
		expect(resumePoint('2026-10-30T12:01:01.5Z')?.since).toBe('2026-10-30T12:00:59Z');
	});

	test('a time that makes no sense gives nothing', () => {
		expect(resumePoint('soon')).toBeNull();
	});
});

describe('dropping repeats after a resume', () => {
	const t = (s: string) => `2026-10-30T12:00:${s}Z`;

	test('counts the lines shown from the key on', () => {
		const lines = [
			makeEntry(1, line('old', { t: t('01.0') })),
			makeEntry(2, line('a', { t: t('05.5') })),
			makeEntry(3, line('b', { t: t('06') }))
		];
		const seen = recentCounts(lines, timeKey(t('05')));
		expect(seen.size).toBe(2);
		expect(takeRepeat(seen, line('old', { t: t('01.0') }))).toBe(false);
		expect(takeRepeat(seen, line('a', { t: t('05.5') }))).toBe(true);
		expect(takeRepeat(seen, line('b', { t: t('06') }))).toBe(true);
	});

	test('each shown line drops one repeat and no more', () => {
		const lines = [
			makeEntry(1, line('.', { t: t('05.5') })),
			makeEntry(2, line('.', { t: t('05.5') }))
		];
		const seen = recentCounts(lines, timeKey(t('05')));
		const again = line('.', { t: t('05.5') });
		expect([takeRepeat(seen, again), takeRepeat(seen, again), takeRepeat(seen, again)]).toEqual([
			true,
			true,
			false
		]);
	});

	test('the same text on the other stream is a different line', () => {
		const seen = recentCounts([makeEntry(1, line('x', { t: t('05.5') }))], timeKey(t('05')));
		expect(takeRepeat(seen, line('x', { s: 2, t: t('05.5') }))).toBe(false);
	});

	test('a new line with the same time is kept', () => {
		const seen = recentCounts([makeEntry(1, line('a', { t: t('05.5') }))], timeKey(t('05')));
		expect(takeRepeat(seen, line('b', { t: t('05.5') }))).toBe(false);
	});

	test('a line with no time is never a repeat', () => {
		const seen = recentCounts([makeEntry(1, line('a'))], timeKey(t('05')));
		expect(takeRepeat(seen, line('a'))).toBe(false);
	});

	test('a line that was cut is recognized when it comes whole', () => {
		const long = 'x'.repeat(maxLineChars + 100);
		const seen = recentCounts([makeEntry(1, line(long, { t: t('05.5') }))], timeKey(t('05')));
		expect(takeRepeat(seen, line(long, { t: t('05.5') }))).toBe(true);
	});
});

describe('logTime', () => {
	test('shows the time of day in the local time zone, to the millisecond', () => {
		const t = new Date(2026, 9, 30, 14, 5, 9, 7).toISOString();
		expect(logTime(t)).toBe('14:05:09.007');
	});

	test('pads', () => {
		expect(logTime(new Date(2026, 0, 2, 1, 2, 3, 40).toISOString())).toBe('01:02:03.040');
	});

	test('nothing for no time', () => {
		expect(logTime(null)).toBe('');
	});

	test('a time that makes no sense is shown as it is', () => {
		expect(logTime('soon')).toBe('soon');
	});
});

describe('scrollRange', () => {
	test('at the top, from row 0 to the rows in view and some more', () => {
		expect(scrollRange(0, 360, 1000, 18)).toEqual({ start: 0, end: 20 + overscan });
	});

	test('scrolled down, starts a little before the first row in view', () => {
		expect(scrollRange(1800, 360, 1000, 18)).toEqual({
			start: 100 - overscan,
			end: 100 + 20 + overscan
		});
	});

	test('a row partly in view counts', () => {
		expect(scrollRange(9, 360, 1000, 18).end).toBe(0 + 20 + overscan);
		expect(scrollRange(19, 361, 1000, 18).end).toBe(1 + 21 + overscan);
	});

	test('does not go past the lines there are', () => {
		expect(scrollRange(1800, 360, 105, 18)).toEqual({ start: 100 - overscan, end: 105 });
	});

	test('scrolled past the end of a list that shrank', () => {
		expect(scrollRange(99_999, 360, 50, 18)).toEqual({ start: 50, end: 50 });
	});

	test('a short list', () => {
		expect(scrollRange(0, 360, 3, 18)).toEqual({ start: 0, end: 3 });
		expect(scrollRange(0, 360, 0, 18)).toEqual({ start: 0, end: 0 });
	});

	test('a bounce above the top counts as the top', () => {
		expect(scrollRange(-40, 360, 100, 18).start).toBe(0);
	});
});

describe('tailRange', () => {
	test('the last rows that fit, and some more', () => {
		expect(tailRange(360, 1000, 18)).toEqual({ start: 1000 - 20 - overscan, end: 1000 });
	});

	test('a short list is all of it', () => {
		expect(tailRange(360, 5, 18)).toEqual({ start: 0, end: 5 });
		expect(tailRange(360, 0, 18)).toEqual({ start: 0, end: 0 });
	});
});
