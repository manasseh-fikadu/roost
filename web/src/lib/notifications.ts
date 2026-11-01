// What the Settings page needs to know about notification channels: the kinds
// there are, how a form turns into a request, and what is wrong with one. The
// hub checks everything again; these only say so sooner.

import type {
	AlertSeverity,
	ChannelInput,
	ChannelKind,
	NotificationChannel,
	QuietHours
} from './api/types.js';

/** A kind of channel, as the picker names it. */
export interface KindInfo {
	value: ChannelKind;
	label: string;
	description: string;
}

/** The kinds of channel, in the order the picker lists them. */
export const kinds: KindInfo[] = [
	{
		value: 'ntfy',
		label: 'ntfy',
		description: 'Push notifications to your phone, from ntfy.sh or your own server.'
	},
	{ value: 'discord', label: 'Discord', description: "A channel's webhook." },
	{ value: 'slack', label: 'Slack', description: 'An incoming webhook.' },
	{ value: 'telegram', label: 'Telegram', description: 'A message from a bot you made.' },
	{ value: 'email', label: 'Email', description: 'Mail through your SMTP server.' },
	{
		value: 'webhook',
		label: 'Webhook',
		description: 'JSON posted to any address, for your own scripts or Home Assistant.'
	}
];

export const kindLabels = Object.fromEntries(kinds.map((k) => [k.value, k.label])) as Record<
	ChannelKind,
	string
>;

/** The least severe alert a channel can hear about, as the form words it. */
export const severityOptions: { value: AlertSeverity; label: string }[] = [
	{ value: 'info', label: 'Every alert' },
	{ value: 'warning', label: 'Warnings and critical' },
	{ value: 'critical', label: 'Only critical' }
];

/** How a channel's routing reads on its card. */
export function routingText(c: Pick<NotificationChannel, 'minSeverity' | 'onResolve'>): string {
	const severity =
		c.minSeverity === 'critical'
			? 'Critical alerts'
			: c.minSeverity === 'warning'
				? 'Warnings and critical alerts'
				: 'Every alert';
	return c.onResolve ? `${severity}, and when they resolve` : severity;
}

/** What a test or a message was about, for "Sent a test 5m ago". */
export const eventNouns: Record<string, string> = {
	opened: 'an alert',
	escalated: 'an escalation',
	reminder: 'a reminder',
	resolved: 'a resolved alert',
	test: 'a test'
};

/** The ways to reach a mail server. */
export const securityOptions = [
	{ value: 'starttls', label: 'STARTTLS (port 587)' },
	{ value: 'tls', label: 'TLS (port 465)' },
	{ value: 'none', label: 'None (port 25)' }
];

/** The secret fields a channel can have, as the API names them. */
export type SecretField = 'url' | 'token' | 'password';

/** A channel's form. Every kind uses some of the fields. */
export interface ChannelDraft {
	name: string;
	kind: ChannelKind;
	enabled: boolean;
	minSeverity: AlertSeverity;
	onResolve: boolean;
	server: string;
	topic: string;
	chatId: string;
	host: string;
	/** NaN while the field is empty. */
	port: number;
	security: string;
	username: string;
	from: string;
	/** The addresses to send mail to, separated by commas, spaces or lines. */
	to: string;
	/** The secrets typed in. Empty keeps the saved one, if there is one. */
	url: string;
	token: string;
	password: string;
	/** The saved secrets to take away. */
	cleared: Record<SecretField, boolean>;
}

/** A form for a new channel of a kind. */
export function newDraft(kind: ChannelKind): ChannelDraft {
	return {
		name: '',
		kind,
		enabled: true,
		minSeverity: 'warning',
		onResolve: true,
		server: '',
		topic: '',
		chatId: '',
		host: '',
		port: NaN,
		security: 'starttls',
		username: '',
		from: '',
		to: '',
		url: '',
		token: '',
		password: '',
		cleared: { url: false, token: false, password: false }
	};
}

/** A form for changing a channel. Its secrets are empty: Bield never sends them. */
export function draftOf(c: NotificationChannel): ChannelDraft {
	return {
		...newDraft(c.kind),
		name: c.name,
		enabled: c.enabled,
		minSeverity: c.minSeverity,
		onResolve: c.onResolve,
		server: c.config.server ?? '',
		topic: c.config.topic ?? '',
		chatId: c.config.chatId ?? '',
		host: c.config.host ?? '',
		port: c.config.port ?? NaN,
		security: c.config.security || 'starttls',
		username: c.config.username ?? '',
		from: c.config.from ?? '',
		to: (c.config.to ?? []).join('\n')
	};
}

/** The addresses in a list someone typed. */
export function recipients(text: string): string[] {
	return text
		.split(/[\s,;]+/)
		.map((s) => s.trim())
		.filter(Boolean);
}

/** Whether a secret field has a value that is saved, or typed and not taken away. */
function hasSecret(d: ChannelDraft, saved: NotificationChannel | null, field: SecretField) {
	if (d[field].trim() !== '') {
		return true;
	}
	return !d.cleared[field] && (saved?.secrets ?? []).includes(field);
}

/**
 * The request that saves a form. A secret that was left empty is left out, so
 * the hub keeps it, and one that is taken away is sent empty.
 */
export function inputOf(d: ChannelDraft): ChannelInput {
	const secret = (field: SecretField): string | undefined => {
		if (d[field] !== '') {
			return d[field];
		}
		return d.cleared[field] ? '' : undefined;
	};
	const email = d.kind === 'email';
	return {
		name: d.name.trim(),
		kind: d.kind,
		enabled: d.enabled,
		minSeverity: d.minSeverity,
		onResolve: d.onResolve,
		config: {
			server: d.server.trim() || undefined,
			topic: d.topic.trim() || undefined,
			chatId: d.chatId.trim() || undefined,
			host: d.host.trim() || undefined,
			port: email && Number.isFinite(d.port) ? d.port : undefined,
			security: email ? d.security : undefined,
			username: d.username.trim() || undefined,
			from: d.from.trim() || undefined,
			to: email ? recipients(d.to) : undefined,
			url: secret('url'),
			token: secret('token'),
			password: secret('password')
		}
	};
}

const topic = /^[A-Za-z0-9_-]{1,64}$/;

/** What is wrong with a form, in a sentence, or '' if nothing is. `saved` is the channel being changed. */
export function channelProblem(d: ChannelDraft, saved: NotificationChannel | null): string {
	if (d.name.trim() === '') {
		return 'Give the channel a name.';
	}
	switch (d.kind) {
		case 'ntfy':
			if (!topic.test(d.topic.trim())) {
				return 'The topic can have letters, digits, - and _, up to 64 of them.';
			}
			if (d.server.trim() !== '' && !/^https?:\/\/[^\s/]+/i.test(d.server.trim())) {
				return 'The server must be a web address, such as https://ntfy.example.com.';
			}
			if ((d.username.trim() !== '') !== hasSecret(d, saved, 'password')) {
				return 'A username needs a password, and a password needs a username.';
			}
			return '';
		case 'discord':
		case 'slack':
			return hasSecret(d, saved, 'url') ? '' : "Paste the webhook's address.";
		case 'webhook':
			if (d.url.trim() !== '' && !/^https?:\/\/[^\s/]+/i.test(d.url.trim())) {
				return 'The address must be a web address, such as https://example.com/hook.';
			}
			return hasSecret(d, saved, 'url') ? '' : "Enter the webhook's address.";
		case 'telegram':
			if (!hasSecret(d, saved, 'token')) {
				return "Paste the bot's token.";
			}
			return d.chatId.trim() === '' ? 'Enter the chat ID to send to.' : '';
		case 'email': {
			if (d.host.trim() === '') {
				return "Enter the mail server's host.";
			}
			if (!Number.isNaN(d.port) && (!Number.isInteger(d.port) || d.port < 1 || d.port > 65535)) {
				return 'The port must be from 1 to 65535.';
			}
			if (!d.from.includes('@')) {
				return 'Enter the address mail comes from.';
			}
			const to = recipients(d.to);
			if (to.length === 0 || to.length > 10) {
				return 'Send mail to 1 to 10 addresses.';
			}
			if (to.some((a) => !/^[^\s@]+@[^\s@]+$/.test(a))) {
				return 'Every address to send mail to must be an email address.';
			}
			if (d.password !== '' && d.username.trim() === '') {
				return 'A password needs a username.';
			}
			return '';
		}
	}
}

/** The time zone this browser is in, such as "Europe/Berlin", or '' if it can't say. */
export function browserTimezone(): string {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone ?? '';
	} catch {
		return '';
	}
}

/** The names of the time zones this browser knows, for a list of suggestions. */
export function timezoneNames(): string[] {
	try {
		return Intl.supportedValuesOf('timeZone');
	} catch {
		return [];
	}
}

function validTimezone(name: string): boolean {
	try {
		new Intl.DateTimeFormat('en', { timeZone: name });
		return true;
	} catch {
		return false;
	}
}

/** What is wrong with the hours between reminders, in a sentence, or '' if nothing is. */
export function remindProblem(hours: number): string {
	return Number.isInteger(hours) && hours >= 0 && hours <= 168
		? ''
		: 'Remind every 1 to 168 hours, or 0 for no reminders.';
}

/** What is wrong with the quiet hours, in a sentence, or '' if nothing is. */
export function quietProblem(q: QuietHours): string {
	const time = /^([01]\d|2[0-3]):[0-5]\d$/;
	if (!time.test(q.start) || !time.test(q.end)) {
		return 'The quiet hours need a start and an end time.';
	}
	if (q.start === q.end) {
		return "The quiet hours can't start when they end.";
	}
	if (q.timezone !== '' && !validTimezone(q.timezone)) {
		return `${q.timezone} isn't the name of a time zone, such as Europe/Berlin.`;
	}
	return '';
}
