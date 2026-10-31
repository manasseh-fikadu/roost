import { describe, expect, test } from 'bun:test';
import type { NotificationChannel, QuietHours } from './api/types.js';
import {
	channelProblem,
	draftOf,
	inputOf,
	newDraft,
	quietProblem,
	recipients,
	remindProblem,
	routingText
} from './notifications.js';

function channel(over: Partial<NotificationChannel> = {}): NotificationChannel {
	return {
		id: 1,
		name: 'Phone',
		kind: 'ntfy',
		enabled: true,
		minSeverity: 'warning',
		onResolve: true,
		config: { topic: 'roost' },
		secrets: [],
		summary: 'ntfy.sh/roost',
		...over
	};
}

describe('inputOf', () => {
	test('leaves out the secrets that were left empty, so the hub keeps them', () => {
		const saved = channel({ kind: 'webhook', config: {}, secrets: ['url', 'token'] });
		const d = draftOf(saved);
		d.name = ' Hook ';
		expect(inputOf(d).config.url).toBeUndefined();
		expect(inputOf(d).config.token).toBeUndefined();
		expect(inputOf(d).name).toBe('Hook');
	});

	test('sends a secret that was typed, and an empty one that was taken away', () => {
		const d = draftOf(channel({ kind: 'webhook', config: {}, secrets: ['url', 'token'] }));
		d.url = 'https://example.com/new';
		d.cleared.token = true;
		const input = inputOf(d);
		expect(input.config.url).toBe('https://example.com/new');
		expect(input.config.token).toBe('');
		expect(input.config.password).toBeUndefined();
	});

	test('sends mail settings only for an email channel', () => {
		const email = newDraft('email');
		Object.assign(email, {
			host: 'smtp.example.com',
			port: 465,
			security: 'tls',
			from: 'a@b.co',
			to: 'x@y.co, z@y.co'
		});
		expect(inputOf(email).config).toMatchObject({
			host: 'smtp.example.com',
			port: 465,
			security: 'tls',
			to: ['x@y.co', 'z@y.co']
		});
		const ntfy = newDraft('ntfy');
		ntfy.topic = 'roost';
		ntfy.port = 25;
		expect(inputOf(ntfy).config.port).toBeUndefined();
		expect(inputOf(ntfy).config.security).toBeUndefined();
		expect(inputOf(ntfy).config.to).toBeUndefined();
	});

	test('leaves the port to the hub while the field is empty', () => {
		const d = newDraft('email');
		expect(inputOf(d).config.port).toBeUndefined();
	});
});

describe('draftOf', () => {
	test('starts from the channel, with no secrets', () => {
		const d = draftOf(
			channel({
				kind: 'email',
				config: { host: 'h', to: ['a@b.co', 'c@d.co'], security: 'tls', port: 465 },
				secrets: ['password']
			})
		);
		expect(d).toMatchObject({
			name: 'Phone',
			host: 'h',
			to: 'a@b.co\nc@d.co',
			security: 'tls',
			port: 465,
			password: ''
		});
	});
});

describe('channelProblem', () => {
	test('wants a name', () => {
		expect(channelProblem(newDraft('webhook'), null)).toBe('Give the channel a name.');
	});

	test('wants what each kind needs', () => {
		const make = (kind: Parameters<typeof newDraft>[0], over: object) => {
			const d = Object.assign(newDraft(kind), { name: 'x' }, over);
			return channelProblem(d, null);
		};
		expect(make('ntfy', {})).toContain('topic');
		expect(make('ntfy', { topic: 'a/b' })).toContain('topic');
		expect(make('ntfy', { topic: 'roost' })).toBe('');
		expect(make('ntfy', { topic: 'roost', server: 'ntfy.sh' })).toContain('web address');
		expect(make('ntfy', { topic: 'roost', username: 'alice' })).toContain('password');
		expect(make('discord', {})).toContain('webhook');
		expect(make('discord', { url: 'https://discord.com/api/webhooks/1/a' })).toBe('');
		expect(make('slack', {})).toContain('webhook');
		expect(make('webhook', { url: 'ftp://x' })).toContain('web address');
		expect(make('webhook', { url: 'https://example.com/h' })).toBe('');
		expect(make('telegram', {})).toContain('token');
		expect(make('telegram', { token: 't' })).toContain('chat');
		expect(make('telegram', { token: 't', chatId: '-100' })).toBe('');
		expect(make('email', {})).toContain('host');
		expect(make('email', { host: 'h' })).toContain('comes from');
		expect(make('email', { host: 'h', from: 'a@b.co' })).toContain('1 to 10');
		expect(make('email', { host: 'h', from: 'a@b.co', to: 'nope' })).toContain('email address');
		expect(make('email', { host: 'h', from: 'a@b.co', to: 'x@y.co', port: 70000 })).toContain(
			'port'
		);
		expect(make('email', { host: 'h', from: 'a@b.co', to: 'x@y.co', password: 'pw' })).toContain(
			'username'
		);
		expect(make('email', { host: 'h', from: 'a@b.co', to: 'x@y.co' })).toBe('');
	});

	test('accepts a saved secret, unless it is taken away', () => {
		const saved = channel({ kind: 'discord', config: {}, secrets: ['url'] });
		const d = draftOf(saved);
		expect(channelProblem(d, saved)).toBe('');
		d.cleared.url = true;
		expect(channelProblem(d, saved)).toContain('webhook');
		d.url = 'https://discord.com/api/webhooks/1/a';
		expect(channelProblem(d, saved)).toBe('');
	});
});

describe('recipients', () => {
	test('splits on commas, semicolons, spaces and lines', () => {
		expect(recipients('a@b.co, c@d.co;e@f.co\n g@h.co ,,')).toEqual([
			'a@b.co',
			'c@d.co',
			'e@f.co',
			'g@h.co'
		]);
		expect(recipients('')).toEqual([]);
	});
});

describe('quietProblem', () => {
	const ok: QuietHours = {
		enabled: true,
		start: '22:00',
		end: '07:00',
		timezone: 'Europe/Berlin',
		allowCritical: true
	};
	test('accepts hours that run through midnight and a time zone', () => {
		expect(quietProblem(ok)).toBe('');
		expect(quietProblem({ ...ok, timezone: '' })).toBe('');
	});
	test('says what is wrong', () => {
		expect(quietProblem({ ...ok, start: '' })).toContain('start and an end');
		expect(quietProblem({ ...ok, end: '7:00' })).toContain('start and an end');
		expect(quietProblem({ ...ok, start: '25:00' })).toContain('start and an end');
		expect(quietProblem({ ...ok, end: '22:00' })).toContain('start when they end');
		expect(quietProblem({ ...ok, timezone: 'Mars/Olympus' })).toContain('Mars/Olympus');
	});
});

describe('remindProblem', () => {
	test('wants 0 to a week of hours', () => {
		for (const h of [0, 1, 24, 168]) {
			expect(remindProblem(h)).toBe('');
		}
		for (const h of [-1, 169, 1.5, NaN]) {
			expect(remindProblem(h)).not.toBe('');
		}
	});
});

describe('routingText', () => {
	test('says what a channel hears', () => {
		expect(routingText({ minSeverity: 'info', onResolve: false })).toBe('Every alert');
		expect(routingText({ minSeverity: 'warning', onResolve: true })).toBe(
			'Warnings and critical alerts, and when they resolve'
		);
		expect(routingText({ minSeverity: 'critical', onResolve: false })).toBe('Critical alerts');
	});
});
