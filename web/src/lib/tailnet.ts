// How the UI describes the tailnet: the path to each device, and how the hub
// reaches the internet.

import type { Device, Status, Uplink } from './api/types.js';
import { latency } from './format.js';

/** A round trip slow enough to point out, in milliseconds. */
export const slowRTT = 400;

/** How traffic gets to a device, in a word or two, such as "direct" or "DERP fra". */
export function linkLabel(d: Device): string {
	const p = d.path;
	switch (p.kind) {
		case 'self':
			return 'this hub';
		case 'direct':
			return 'direct';
		case 'derp':
			return p.derp ? `DERP ${p.derp}` : 'DERP';
		case 'peer-relay':
			return 'peer relay';
		case 'offline':
			return 'not observed';
	}
}

/** The path to a device and its round trip, such as "direct · 5.2 ms". */
export function pathLabel(d: Device): string {
	const link = linkLabel(d);
	return d.path.rttMs === undefined ? link : `${link} · ${latency(d.path.rttMs)}`;
}

/** Whether the link to a device is slow, or didn't answer its last ping. */
export function slowLink(d: Device): boolean {
	return d.online && (d.path.unanswered === true || (d.path.rttMs ?? 0) > slowRTT);
}

/** A device's IPv4 address on the tailnet, or else its first address. */
export function tailnetIP(d: Device): string | undefined {
	return d.ips.find((ip) => !ip.includes(':')) ?? d.ips[0];
}

/** Whether the hub reaches the internet, from the latest connectivity check. */
export function uplinkStatus(u: Uplink): Status {
	if (!u.checkedAt) {
		return 'unknown';
	}
	if (!u.ipv4 && !u.ipv6) {
		return 'offline';
	}
	// Without UDP, every connection is relayed.
	return u.udp ? 'online' : 'attention';
}

/** Whether the hub reaches one of Tailscale's DERP relays. */
export function derpStatus(u: Uplink): Status {
	if (u.derp) {
		return 'online';
	}
	return u.checkedAt ? 'offline' : 'unknown';
}
