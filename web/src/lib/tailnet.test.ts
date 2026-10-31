import { describe, expect, test } from 'bun:test';
import type { Device, Path, Status, Uplink } from './api/types.js';
import { derpStatus, pathLabel, slowLink, tailnetIP, uplinkStatus } from './tailnet.js';

function device(path: Path, online = true, ips = ['100.64.0.2', 'fd7a:115c:a1e0::2']): Device {
	return {
		id: 'laptop',
		name: 'laptop',
		hostName: 'laptop',
		dnsName: 'laptop.example.ts.net',
		os: 'macOS',
		kind: 'laptop',
		ips,
		online,
		status: online ? 'online' : 'offline',
		statusReason: '',
		path,
		rxBytes: 0,
		txBytes: 0,
		docker: false,
		settings: { displayName: '', alertOffline: true, thresholds: {}, notes: '' }
	};
}

describe('pathLabel', () => {
	test.each<{ name: string; path: Path; want: string }>([
		{ name: 'the hub', path: { kind: 'self' }, want: 'this hub' },
		{
			name: 'direct with a round trip',
			path: { kind: 'direct', rttMs: 5.24 },
			want: 'direct · 5.2 ms'
		},
		{
			name: 'through DERP',
			path: { kind: 'derp', derp: 'fra', rttMs: 80 },
			want: 'DERP fra · 80 ms'
		},
		{ name: 'DERP without a region', path: { kind: 'derp' }, want: 'DERP' },
		{ name: 'through a peer relay', path: { kind: 'peer-relay' }, want: 'peer relay' },
		{ name: 'offline', path: { kind: 'offline' }, want: 'not observed' }
	])('$name', ({ path, want }) => {
		expect(pathLabel(device(path))).toBe(want);
	});
});

describe('slowLink', () => {
	test.each<{ name: string; path: Path; online?: boolean; want: boolean }>([
		{ name: 'fast', path: { kind: 'direct', rttMs: 20 }, want: false },
		{ name: 'slow', path: { kind: 'derp', rttMs: 480 }, want: true },
		{ name: 'unanswered', path: { kind: 'direct', unanswered: true }, want: true },
		{
			name: 'offline isn’t slow',
			path: { kind: 'offline', rttMs: 900 },
			online: false,
			want: false
		}
	])('$name', ({ path, online, want }) => {
		expect(slowLink(device(path, online))).toBe(want);
	});
});

describe('tailnetIP', () => {
	test.each<{ name: string; ips: string[]; want: string | undefined }>([
		{ name: 'IPv4 first', ips: ['fd7a:115c:a1e0::2', '100.64.0.2'], want: '100.64.0.2' },
		{ name: 'IPv6 if that’s all', ips: ['fd7a:115c:a1e0::2'], want: 'fd7a:115c:a1e0::2' },
		{ name: 'none', ips: [], want: undefined }
	])('$name', ({ ips, want }) => {
		expect(tailnetIP(device({ kind: 'direct' }, true, ips))).toBe(want);
	});
});

describe('uplink', () => {
	const checkedAt = '2026-10-22T12:00:00Z';
	test.each<{ name: string; uplink: Uplink; internet: Status; derp: Status }>([
		{
			name: 'not checked yet',
			uplink: { udp: false, ipv4: false, ipv6: false },
			internet: 'unknown',
			derp: 'unknown'
		},
		{
			name: 'all good',
			uplink: {
				checkedAt,
				udp: true,
				ipv4: true,
				ipv6: false,
				derp: { code: 'fra', name: 'Frankfurt', latencyMs: 3.8 }
			},
			internet: 'online',
			derp: 'online'
		},
		{
			name: 'UDP blocked',
			uplink: { checkedAt, udp: false, ipv4: true, ipv6: true },
			internet: 'attention',
			derp: 'offline'
		},
		{
			name: 'no internet',
			uplink: { checkedAt, udp: false, ipv4: false, ipv6: false },
			internet: 'offline',
			derp: 'offline'
		}
	])('$name', ({ uplink, internet, derp }) => {
		expect(uplinkStatus(uplink)).toBe(internet);
		expect(derpStatus(uplink)).toBe(derp);
	});
});
