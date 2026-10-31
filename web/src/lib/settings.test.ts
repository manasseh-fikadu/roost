import { describe, expect, test } from 'bun:test';
import type { InstalledService, RetentionSettings, Thresholds } from './api/types.js';
import {
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
		expect(retentionProblem({ ...kept, historyDays: 31 })).toContain('device history');
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
		'https://roost.example.ts.net',
		'http://100.64.0.1:4700',
		'HTTPS://Roost.Example.com',
		'https://roost.example.ts.net/',
		'  https://roost.example.ts.net  ',
		'http://[fd7a:115c:a1e0::1]:8080'
	])('accepts %j', (url) => {
		expect(publicUrlProblem(url)).toBe('');
	});

	test.each([
		['no scheme', 'roost.example.com', 'web address'],
		['another scheme', 'ftp://roost.example.com', 'web address'],
		['a scheme alone', 'https://', 'web address'],
		['no host', 'https://:8080', 'web address'],
		['a bad port', 'https://example.com:99999', 'web address'],
		['a path', 'https://example.com/roost', 'scheme, a host and a port'],
		['two slashes at the end', 'https://example.com//', 'scheme, a host and a port'],
		['a query', 'https://example.com?a=b', 'scheme, a host and a port'],
		['a fragment', 'https://example.com#top', 'scheme, a host and a port'],
		['a login', 'https://alice:secret@example.com', 'scheme, a host and a port'],
		['a backslash', 'https://example.com\\roost', 'scheme, a host and a port'],
		['a space in the host', 'https://exa mple.com', 'spaces'],
		['a line break', 'https://example.com\nhttp://x', 'spaces']
	])('rejects %s', (_, url, words) => {
		expect(publicUrlProblem(url)).toContain(words);
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
		['app@1', 'app@1.service']
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
		['a user: prefix', 'user:app.service', 'User']
	])('rejects %s', (_, input, words) => {
		expect(readUnitName(input).problem).toContain(words);
	});
});

describe('loginProblem', () => {
	test.each(['alice@example.com', ' alice@github ', 'a@b'])('accepts %j', (login) => {
		expect(loginProblem(login)).toBe('');
	});

	test.each(['', '   ', 'alice', '@example.com', 'alice@', 'a@b@c', 'alice smith@example.com'])(
		'rejects %j',
		(login) => {
			expect(loginProblem(login)).toContain('Tailscale login');
		}
	);
});
