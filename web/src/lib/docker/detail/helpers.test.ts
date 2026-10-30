import { describe, expect, test } from 'bun:test';
import {
	checkDuration,
	composeFileNames,
	crash,
	envRows,
	filterRows,
	healthCommand,
	healthFacts,
	knownTime,
	jsonTokens,
	labelRows,
	millis,
	parseTab,
	restartPolicyLabel,
	searchJson,
	secondsSince,
	shortWhen,
	splitPorts,
	statsEnded,
	stripAnsi,
	volumeName
} from './helpers.js';

describe('parseTab', () => {
	test.each([
		['logs', 'logs'],
		['inspect', 'inspect'],
		['overview', 'overview'],
		['nope', 'overview'],
		['', 'overview'],
		[null, 'overview'],
		[undefined, 'overview']
	] as const)('%p is %p', (value, want) => {
		expect(parseTab(value)).toBe(want);
	});
});

describe('secondsSince', () => {
	const now = Date.parse('2027-03-01T12:00:00Z');
	test('counts the seconds', () => {
		expect(secondsSince('2027-03-01T11:59:00Z', now)).toBe(60);
	});
	test('is never negative', () => {
		expect(secondsSince('2027-03-01T12:00:30Z', now)).toBe(0);
	});
	test.each([[undefined], [null], [''], ['not a time']] as const)('has none for %p', (t) => {
		expect(secondsSince(t, now)).toBeNull();
	});
});

describe('shortWhen', () => {
	const now = new Date(2027, 2, 1, 12, 0, 0).getTime();
	test('gives a time of day for today', () => {
		const today = new Date(2027, 2, 1, 9, 5, 0).toISOString();
		expect(shortWhen(today, now)).toMatch(/9:05/);
	});
	test('gives a date for an earlier day', () => {
		const before = new Date(2027, 1, 14, 9, 5, 0).toISOString();
		const text = shortWhen(before, now);
		expect(text).toContain('14');
		expect(text).not.toMatch(/9:05/);
	});
	test.each([[undefined], [null], ['not a time']] as const)('has a dash for %p', (t) => {
		expect(shortWhen(t, now)).toBe('—');
	});
});

describe('millis', () => {
	test.each([
		[0, '0ms'],
		[250, '250ms'],
		[999.6, '1s'],
		[1000, '1s'],
		[1500, '1.5s'],
		[30000, '30s'],
		[null, null],
		[undefined, null],
		[Number.NaN, null]
	] as const)('%p is %p', (ms, want) => {
		expect(millis(ms)).toBe(want);
	});
});

describe('healthCommand', () => {
	test.each([
		[
			'a shell command',
			['CMD-SHELL', 'curl -f http://localhost/ || exit 1'],
			'curl -f http://localhost/ || exit 1'
		],
		['an argument list', ['CMD', 'pg_isready', '-U', 'my user'], 'pg_isready -U "my user"'],
		['another form', ['pg_isready'], 'pg_isready'],
		['nothing, for a viewer', [], '']
	])('reads %s', (_, test, want) => {
		expect(healthCommand(test)).toBe(want);
	});
	test('reads a missing test', () => {
		expect(healthCommand(undefined)).toBe('');
	});
});

describe('healthFacts', () => {
	test('says what is set', () => {
		expect(
			healthFacts({ test: [], interval: 30000, timeout: 3000, retries: 3, startPeriod: 1500 })
		).toEqual(['every 30s', 'timeout 3s', '3 retries', '1.5s start period']);
	});
	test('leaves out what is not', () => {
		expect(healthFacts({ interval: 5000, retries: 1 })).toEqual(['every 5s', '1 retry']);
		expect(healthFacts({ interval: 0 })).toEqual([]);
		expect(healthFacts(undefined)).toEqual([]);
	});
});

describe('checkDuration', () => {
	test('is how long the check took', () => {
		expect(
			checkDuration({ start: '2027-03-01T12:00:00.000Z', end: '2027-03-01T12:00:00.250Z' })
		).toBe(250);
	});
	test('has none for times that do not say', () => {
		expect(
			checkDuration({ start: '0001-01-01T00:00:00Z', end: '2027-03-01T12:00:00Z' })
		).toBeNull();
		expect(
			checkDuration({ start: '2027-03-01T12:00:01Z', end: '2027-03-01T12:00:00Z' })
		).toBeNull();
		expect(checkDuration({ start: 'x', end: 'y' })).toBeNull();
	});
});

describe('knownTime', () => {
	test.each([
		['2027-03-01T12:00:00Z', true],
		['0001-01-01T00:00:00Z', false],
		['', false],
		['x', false],
		[undefined, false]
	] as const)('%p is %p', (t, want) => {
		expect(knownTime(t)).toBe(want);
	});
});

describe('stripAnsi', () => {
	test('takes out colors', () => {
		expect(stripAnsi('\u001b[31mfailed\u001b[0m: \u001b[1;33mretry\u001b[m')).toBe('failed: retry');
	});
	test('leaves plain text', () => {
		expect(stripAnsi('a [b] c')).toBe('a [b] c');
	});
});

describe('crash', () => {
	const base = { running: false };
	test('has nothing for a container that stopped cleanly', () => {
		expect(crash({ ...base, exitCode: 0, oomKilled: false })).toBeNull();
		expect(crash({ ...base, exitCode: 143, oomKilled: false })).toBeNull();
		expect(crash({ running: true, oomKilled: false })).toBeNull();
	});
	test('says an exit code', () => {
		expect(
			crash({ ...base, exitCode: 1, oomKilled: false, finishedAt: '2027-03-01T00:00:00Z' })
		).toEqual({
			title: 'Exited with code 1',
			error: undefined,
			at: '2027-03-01T00:00:00Z'
		});
	});
	test('says it ran out of memory before anything else', () => {
		expect(crash({ ...base, exitCode: 137, oomKilled: true })?.title).toBe(
			'Killed for running out of memory'
		);
	});
	test('shows Docker’s error even if the exit code was clean', () => {
		const c = crash({ ...base, exitCode: 0, oomKilled: false, error: 'no such file' });
		expect(c?.title).toBe("Docker couldn't run it");
		expect(c?.error).toBe('no such file');
	});
});

describe('splitPorts', () => {
	test('tells published from exposed', () => {
		const ports = [
			{ private: 80, public: 8080, type: 'tcp' },
			{ private: 443, type: 'tcp' }
		];
		const { published, exposed } = splitPorts(ports);
		expect(published.map((p) => p.private)).toEqual([80]);
		expect(exposed.map((p) => p.private)).toEqual([443]);
	});
});

describe('composeFileNames', () => {
	test('keeps the names', () => {
		expect(composeFileNames('/srv/app/compose.yaml, /srv/app/compose.prod.yaml')).toBe(
			'compose.yaml, compose.prod.yaml'
		);
		expect(composeFileNames(undefined)).toBe('');
	});
});

describe('volumeName', () => {
	test('shortens an anonymous volume', () => {
		expect(volumeName('a'.repeat(64))).toBe(`${'a'.repeat(12)}…`);
	});
	test('keeps a name', () => {
		expect(volumeName('pgdata')).toBe('pgdata');
		expect(volumeName(undefined)).toBe('');
	});
});

describe('restartPolicyLabel', () => {
	test.each([
		[undefined, false, 'no'],
		[{ name: 'no' }, false, 'no'],
		[{ name: 'always' }, false, 'always'],
		[{ name: 'on-failure', maxRetries: 3 }, false, 'on-failure (max 3)'],
		[{ name: 'on-failure', maxRetries: 0 }, false, 'on-failure'],
		[{ name: 'unless-stopped' }, true, 'unless-stopped · auto-remove']
	] as const)('%p with auto-remove %p is %p', (policy, auto, want) => {
		expect(restartPolicyLabel(policy, auto)).toBe(want);
	});
});

describe('statsEnded', () => {
	test('keeps the stream’s reason while the container runs', () => {
		expect(statsEnded({ running: true, paused: false }, null)).toBeNull();
		expect(statsEnded({ running: true, paused: false }, 'The container stopped')).toBe(
			'The container stopped'
		);
	});
	test('says why there are no stats', () => {
		expect(statsEnded({ running: false, paused: false }, null)).toBe("The container isn't running");
		expect(statsEnded({ running: true, paused: true }, null)).toBe('The container is paused');
	});
});

describe('envRows', () => {
	test('sorts by name and hides the values', () => {
		const rows = envRows([
			{ key: 'PORT', value: '80' },
			{ key: 'DB_URL', value: 'postgres://u:p@h/db' },
			{ key: 'EMPTY', value: '' }
		]);
		expect(rows.map((r) => r.key)).toEqual(['DB_URL', 'EMPTY', 'PORT']);
		expect(rows.map((r) => r.secret)).toEqual([true, false, true]);
	});
	test('gives repeated names their own ids', () => {
		const rows = envRows([
			{ key: 'A', value: '1' },
			{ key: 'A', value: '2' }
		]);
		expect(new Set(rows.map((r) => r.id)).size).toBe(2);
	});
	test('is empty for a viewer', () => {
		expect(envRows(undefined)).toEqual([]);
	});
});

describe('labelRows', () => {
	test('hides only the labels that look like credentials', () => {
		const rows = labelRows({ 'com.example.team': 'infra' }, { 'com.example.api_token': 'abc' });
		expect(rows.map((r) => [r.key, r.secret])).toEqual([
			['com.example.api_token', true],
			['com.example.team', false]
		]);
	});
	test('is the labels alone for a viewer', () => {
		expect(labelRows({ a: '1' }, undefined)).toHaveLength(1);
		expect(labelRows({ a: '1' }, {})).toHaveLength(1);
	});
});

describe('filterRows', () => {
	const rows = envRows([
		{ key: 'DATABASE_URL', value: 'postgres://db' },
		{ key: 'PORT', value: '8080' }
	]);
	test('matches a name or a value, ignoring case', () => {
		expect(filterRows(rows, 'database').map((r) => r.key)).toEqual(['DATABASE_URL']);
		expect(filterRows(rows, '80').map((r) => r.key)).toEqual(['PORT']);
	});
	test('keeps everything for an empty filter', () => {
		expect(filterRows(rows, '  ')).toBe(rows);
	});
});

describe('searchJson', () => {
	const doc = {
		Id: 'abc123',
		Config: { Env: ['PORT=80', 'MODE=prod'], Labels: { 'com.docker.compose.project': 'shop' } },
		State: { Running: true, Pid: 42 },
		Mounts: [],
		Args: null
	};
	test('finds values by their text', () => {
		const { hits, total } = searchJson(doc, 'prod');
		expect(total).toBe(1);
		expect(hits[0]).toEqual({ path: 'Config.Env[1]', text: '"MODE=prod"' });
	});
	test('finds values by their path', () => {
		const { hits } = searchJson(doc, 'state');
		expect(hits.map((h) => h.path)).toEqual(['State.Running', 'State.Pid']);
		expect(hits[0]?.text).toBe('true');
	});
	test('writes a path with dots in a name in brackets', () => {
		const { hits } = searchJson(doc, 'shop');
		expect(hits[0]?.path).toBe('Config.Labels["com.docker.compose.project"]');
	});
	test('finds empty lists and null', () => {
		expect(searchJson(doc, 'mounts').hits[0]).toEqual({ path: 'Mounts', text: '[]' });
		expect(searchJson(doc, 'args').hits[0]).toEqual({ path: 'Args', text: 'null' });
	});
	test('stops at the limit and still counts', () => {
		const { hits, total } = searchJson({ a: ['x', 'x', 'x', 'x'] }, 'x', 2);
		expect(hits).toHaveLength(2);
		expect(total).toBe(4);
	});
	test('finds nothing for an empty query', () => {
		expect(searchJson(doc, ' ')).toEqual({ hits: [], total: 0 });
	});
	test('starts a path with an index', () => {
		expect(searchJson(['a', 'b'], 'b').hits[0]?.path).toBe('[1]');
	});
});

describe('jsonTokens', () => {
	test('gives every part its kind', () => {
		const text = JSON.stringify({ a: 'x 1', b: [true, null, -2.5e3] }, null, 2);
		const tokens = jsonTokens(text).filter((t) => t.kind !== 'plain');
		expect(tokens).toEqual([
			{ text: '"a"', kind: 'key' },
			{ text: '"x 1"', kind: 'string' },
			{ text: '"b"', kind: 'key' },
			{ text: 'true', kind: 'literal' },
			{ text: 'null', kind: 'literal' },
			{ text: '-2500', kind: 'number' }
		]);
	});
	test('puts the text back together', () => {
		const text = JSON.stringify({ q: 'say "hi" 42 true', n: [1, 2], o: {} }, null, 2);
		expect(
			jsonTokens(text)
				.map((t) => t.text)
				.join('')
		).toBe(text);
	});
	test('does not color what is inside a string', () => {
		const tokens = jsonTokens('"true 12"');
		expect(tokens).toEqual([{ text: '"true 12"', kind: 'string' }]);
	});
});
