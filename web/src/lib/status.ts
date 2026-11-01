// How Bield shows the state of a device, service or check.

import type { Status } from './api/types.js';

/** The state of something Bield watches. The hub sends it. */
export type { Status };

/** A color for badges and pills. */
export type Tone = 'green' | 'amber' | 'red' | 'blue' | 'purple' | 'gray';

interface StatusMeta {
	/** The state in a word, for sentences and labels. */
	label: string;
	/** The state in capitals, for status pills. */
	badge: string;
	tone: Tone;
	/** The background class of the state's dot. */
	dot: string;
	/** The text class of the state's color. */
	text: string;
}

export const statusMeta: Record<Status, StatusMeta> = {
	online: {
		label: 'Online',
		badge: 'LIVE',
		tone: 'green',
		dot: 'bg-green-fg',
		text: 'text-green-fg'
	},
	attention: {
		label: 'Attention',
		badge: 'ATTENTION',
		tone: 'amber',
		dot: 'bg-amber',
		text: 'text-amber-fg'
	},
	offline: {
		label: 'Offline',
		badge: 'OFFLINE',
		tone: 'red',
		dot: 'bg-red-fg',
		text: 'text-red-fg'
	},
	unknown: {
		label: 'Unknown',
		badge: 'UNKNOWN',
		tone: 'gray',
		dot: 'bg-fg-faint',
		text: 'text-fg-subtle'
	}
};

export const toneClass: Record<Tone, string> = {
	green: 'border-green/30 bg-green/10 text-green-fg',
	amber: 'border-amber/30 bg-amber/10 text-amber-fg',
	red: 'border-red/35 bg-red/10 text-red-fg',
	blue: 'border-blue-fg/30 bg-blue/10 text-blue-fg',
	purple: 'border-purple-fg/30 bg-purple-fg/10 text-purple-fg',
	gray: 'border-line-strong bg-hover text-fg-muted'
};
