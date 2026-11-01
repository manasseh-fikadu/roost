import { describe, expect, test } from 'bun:test';
import type { InstalledService, RetentionSettings, Thresholds } from './api/types.js';
import {
	changedFields,
	follow,
	loginProblem,
	matchServices,
	numberOf,
	parseUnit,
	publicUrlProblem,
	readUnitName,
	retentionProblem,
	same,
	sameSet,
	shortensRetention,
	thresholdsProblem,
	unitKey
} from './settings.js';

const defaults: Thresholds = {
	diskWarn: 85,
	diskCritical: 95,
	memoryWarn: 92,
	cpuWarn: 90,
	cpuWindowMinutes: 5,
	offlineMinutes: 10,
	keyExpiryDays: 14,
	checkFailures: 2
};

describe('thresholdsProblem', () => {
	test('accepts the defaults', () => {
		expect(thresholdsProblem(defaults)).toBe('');
	});

	test.each([
		['a missing percentage', { memoryWarn: NaN }, 'memory'],
		['a percentage over 100', { cpuWarn: 101 }, 'CPU'],
		['a disk critical before its warning', { diskCritical: 80 }, 'critical before'],
		['no CPU window', { cpuWindowMinutes: 0 }, 'stay busy'],
		['a CPU window over an hour', { cpuWindowMinutes: 61 }, 'stay busy'],
		['a fraction of a minute', { offlineMinutes: 1.5 }, 'offline'],
		['a key warning of no days', { keyExpiryDays: 0 }, 'key expires'],
		['no failures', { checkFailures: 0 }, 'failures']
	] as [string, Partial<Thresholds>, string][])('rejects %s', (_, change, words) => {
		expect(thresholdsProblem({ ...defaults, ...change })).toContain(words);
	});

	test('allows the edges', () => {
		expect(
			thresholdsProblem({
				...defaults,
				diskWarn: 100,
				diskCritical: 100,
				cpuWindowMinutes: 60,
				offlineMinutes: 10080
			})
		).toBe('');
	});
});

describe('retention', () => {
	const kept: RetentionSettings = { activityDays: 30, historyDays: 7, checksDays: 7 };

	test('retentionProblem', () => {
		expect(retentionProblem(kept)).toBe('');
		expect(retentionProblem({ ...kept, historyDays: 91 })).toContain('device history');
		expect(retentionProblem({ ...kept, activityDays: 0 })).toContain('activity log');
		expect(retentionProblem({ ...kept, checksDays: 90.5 })).toContain('check results');
	});

	test('shortensRetention', () => {
		expect(shortensRetention(kept, kept)).toBe(false);
		expect(shortensRetention(kept, { ...kept, activityDays: 90 })).toBe(false);
		expect(shortensRetention(kept, { ...kept, historyDays: 3 })).toBe(true);
	});
});

test('same compares by value', () => {
	expect(same({ a: [1, 2] }, { a: [1, 2] })).toBe(true);
	expect(same({ a: [1, 2] }, { a: [2, 1] })).toBe(false);
});

test('changedFields keeps what was not edited out of a change', () => {
	const saved = { a: 1, b: 'x', c: [1, 2] };
	expect(changedFields({ ...saved }, saved)).toEqual({});
	expect(changedFields({ a: 2, b: 'x', c: [1, 2] }, saved)).toEqual({ a: 2 });
	expect(changedFields({ a: 1, b: '', c: [1, 3] }, saved)).toEqual({ b: '', c: [1, 3] });
});

test('follow keeps what was edited and takes the rest from the saved settings', () => {
	const was = {
		thresholds: { disk: 85, memory: 92, cpu: 90 },
		rules: { disk: true, cpu: true },
		units: ['a']
	};
	// The admin edited the CPU threshold and the disk rule.
	const edited = {
		thresholds: { disk: 85, memory: 92, cpu: 75 },
		rules: { disk: false, cpu: true },
		units: ['a']
	};
	// Meanwhile someone changed memory, the CPU threshold and the CPU rule, and added a unit.
	const now = {
		thresholds: { disk: 85, memory: 70, cpu: 80 },
		rules: { disk: true, cpu: false },
		units: ['a', 'b']
	};
	expect(follow(edited, was, now)).toEqual({
		thresholds: { disk: 85, memory: 70, cpu: 75 },
		rules: { disk: false, cpu: false },
		units: ['a', 'b']
	});
	// A draft nobody edited is the saved settings.
	expect(follow(was, was, now)).toEqual(now);
	// What isn't in the edit is added from the saved settings.
	expect(follow({ a: 1 }, { a: 1 }, { a: 2, b: 3 })).toEqual({ a: 2, b: 3 });
});

test('numberOf', () => {
	expect(numberOf(5)).toBe(5);
	expect(numberOf('7.5')).toBe(7.5);
	expect(numberOf('')).toBeUndefined();
	expect(numberOf(null)).toBeUndefined();
	expect(numberOf('abc')).toBeUndefined();
	expect(numberOf(0)).toBe(0);
});

test('units', () => {
	expect(parseUnit('user:app.service')).toEqual({ name: 'app.service', user: true });
	expect(parseUnit('ssh.service')).toEqual({ name: 'ssh.service', user: false });
	expect(unitKey('app.service', true)).toBe('user:app.service');
	expect(unitKey('ssh.service', false)).toBe('ssh.service');
});

test('matchServices', () => {
	const services: InstalledService[] = [
		{
			key: 'ssh.service',
			name: 'ssh.service',
			scope: 'system',
			description: 'OpenBSD Secure Shell server'
		},
		{ key: 'nginx.service', name: 'nginx.service', scope: 'system', active: 'failed' },
		{ key: 'user:app.service', name: 'app.service', scope: 'user', description: 'My app' }
	];
	const keys = (q: string) => matchServices(services, q).map((s) => s.key);
	expect(keys('')).toEqual(['ssh.service', 'nginx.service', 'user:app.service']);
	expect(keys('SSH')).toEqual(['ssh.service']);
	expect(keys('secure shell')).toEqual(['ssh.service']);
	expect(keys('shell nginx')).toEqual([]);
	expect(keys('user')).toEqual(['user:app.service']);
	expect(keys('zzz')).toEqual([]);
});

describe('publicUrlProblem', () => {
	test.each([
		'',
		'   ',
		'https://bield.example.ts.net',
		'http://100.64.0.1:4700',
		'HTTPS://Bield.Example.com',
		'https://bield.example.ts.net/',
		'  https://bield.example.ts.net  ',
		'http://[fd7a:115c:a1e0::1]:8080',
		'http://[::1]',
		'https://localhost:1',
		'https://bield.example.com:65535',
		'https://my-host.example.com',
		'https://a.b.c.d.example',
		`https://${'a.'.repeat(126)}a`
	])('accepts %j', (url) => {
		expect(publicUrlProblem(url)).toBe('');
	});

	test.each([
		['no scheme', 'bield.example.com', 'web address'],
		['another scheme', 'ftp://bield.example.com', 'web address'],
		['a scheme alone', 'https://', 'web address'],
		['an unclosed bracket', 'http://[::1', 'web address'],
		['text after a bracket', 'http://[::1]x', 'web address'],
		['a path', 'https://example.com/bield', 'scheme, a host and a port'],
		['two slashes at the end', 'https://example.com//', 'scheme, a host and a port'],
		['a query', 'https://example.com?a=b', 'scheme, a host and a port'],
		['a fragment', 'https://example.com#top', 'scheme, a host and a port'],
		['a login', 'https://alice:secret@example.com', 'scheme, a host and a port'],
		['a backslash', 'https://example.com\\bield', 'scheme, a host and a port'],
		['a space in the host', 'https://exa mple.com', 'spaces'],
		['a line break', 'https://example.com\nhttp://x', 'spaces'],
		['a no-break space in the host', 'https://exa\u00a0mple.com', 'spaces']
	])('rejects %s', (_, url, words) => {
		expect(publicUrlProblem(url)).toContain(words);
	});

	test.each([
		['no host', 'https://:8080'],
		['a dollar sign in the host', 'https://exa$mple.com'],
		['a semicolon in the host', 'https://exa;mple.com'],
		['an apostrophe in the host', "https://exa'mple.com"],
		['a quote in the host', 'https://exa"mple.com'],
		['an underscore in the host', 'https://exa_mple.com'],
		['a percent sign in the host', 'https://exa%41mple.com'],
		['a zero-width space in the host', 'https://exa\u200bmple.com'],
		['a soft hyphen in the host', 'https://exa\u00admple.com'],
		['an accented letter in the host', 'https://b\u00fccher.example'],
		['a dash first in a label', 'https://-example.com'],
		['a dash last in a label', 'https://example-.com'],
		['an empty label', 'https://example..com'],
		['a dot first', 'https://.example.com'],
		['a dot last', 'https://example.com.'],
		['a host over 253 characters', `https://${'a.'.repeat(126)}aa`],
		['an empty port', 'https://example.com:'],
		['port zero', 'https://example.com:0'],
		['a port over 65535', 'https://example.com:65536'],
		['a very large port', 'https://example.com:99999'],
		['a port that is not a number', 'https://example.com:abc'],
		['a signed port', 'https://example.com:+80'],
		['an empty port after a bracket', 'http://[::1]:'],
		['a zone on an IPv6 address', 'http://[fe80::1%25eth0]'],
		['a bare zone on an IPv6 address', 'http://[fe80::1%eth0]'],
		['an IPv6 address that is not one', 'http://[not:an:ip]'],
		['an IPv6 address without brackets', 'http://::1'],
		['empty brackets', 'http://[]']
	])('rejects %s', (_, url) => {
		expect(publicUrlProblem(url)).toContain('name or an IP address');
	});
});

test('sameSet ignores order and repeats', () => {
	expect(sameSet(['a', 'b'], ['b', 'a'])).toBe(true);
	expect(sameSet(['a', 'a'], ['a'])).toBe(true);
	expect(sameSet([], [])).toBe(true);
	expect(sameSet(['a'], ['a', 'b'])).toBe(false);
	expect(sameSet(['a', 'b'], ['a', 'c'])).toBe(false);
});

describe('readUnitName', () => {
	test.each([
		['nginx', 'nginx.service'],
		['  nginx.service ', 'nginx.service'],
		['backup.timer', 'backup.timer'],
		['app@1', 'app@1.service'],
		['getty@tty1.service', 'getty@tty1.service'],
		['a:b_c-d\\e.service', 'a:b_c-d\\e.service'],
		[`${'a'.repeat(247)}.service`, `${'a'.repeat(247)}.service`]
	])('reads %j as %j', (input, name) => {
		expect(readUnitName(input)).toEqual({ name, problem: '' });
	});

	test('empty input is nothing, not a problem', () => {
		expect(readUnitName('')).toEqual({ name: '', problem: '' });
		expect(readUnitName('   ')).toEqual({ name: '', problem: '' });
	});

	test.each([
		['a space', 'my app', 'spaces'],
		['a slash', 'a/b.service', 'slashes'],
		['a leading dash', '-x.service', 'dash'],
		['a user: prefix', 'user:app.service', 'User'],
		['an accent', 'café.service', 'letters, digits'],
		['a plus sign', 'a+b.service', 'letters, digits'],
		['a template', 'getty@.service', 'template'],
		['a template without its suffix', 'getty@', 'template'],
		['a name over 255 characters', `${'a'.repeat(248)}.service`, '255']
	])('rejects %s', (_, input, words) => {
		expect(readUnitName(input).problem).toContain(words);
	});
});

describe('loginProblem', () => {
	test.each(['alice@example.com', ' alice@github ', 'a@b', 'alice', 'https://idp.example.com/u/1'])(
		'accepts %j',
		(login) => {
			expect(loginProblem(login)).toBe('');
		}
	);

	test.each(['', '   ', 'alice smith@example.com', 'alice\tsmith'])('rejects %j', (login) => {
		expect(loginProblem(login)).toContain('without spaces');
	});
});
