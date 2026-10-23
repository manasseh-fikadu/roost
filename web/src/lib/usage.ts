// How the UI shows what uses a device's CPU and memory.

import type { UsageHistory, UsageKind } from './api/types.js';
import { bytes } from './format.js';
import type { Tone } from './status.js';

/** What the usage panel ranks by. */
export type UsageMetric = 'cpu' | 'mem';

const kindLabels: Record<UsageKind, string> = {
	container: 'container',
	service: 'service',
	'user-service': 'user service',
	session: 'session',
	kernel: 'kernel',
	app: 'app',
	other: 'other'
};

/** A kind of group's name, such as "user service". */
export function kindLabel(kind: UsageKind): string {
	return kindLabels[kind];
}

const kindTones: Record<UsageKind, Tone> = {
	container: 'blue',
	service: 'gray',
	'user-service': 'purple',
	session: 'gray',
	kernel: 'gray',
	app: 'blue',
	other: 'gray'
};

/** The badge color of a kind of group. */
export function kindTone(kind: UsageKind): Tone {
	return kindTones[kind];
}

/** A share of the CPU, such as 0%, <0.1%, 4.2% or 37%. */
export function cpuText(v: number | null | undefined): string {
	if (v == null || !Number.isFinite(v)) {
		return '—';
	}
	if (v === 0) {
		return '0%';
	}
	if (v < 0.1) {
		return '<0.1%';
	}
	return `${v.toFixed(v >= 10 ? 0 : 1)}%`;
}

/** A value of a metric: a share of the CPU, or bytes of memory. */
export function usageValue(metric: UsageMetric, v: number): string {
	return metric === 'cpu' ? cpuText(v) : bytes(v);
}

/**
 * Colors for charting groups, in an order that people with color vision
 * deficiencies can still tell apart on the dark theme.
 */
const seriesPalette = [
	'#3987e5',
	'#d95926',
	'#199e70',
	'#c98500',
	'#d55181',
	'#008300',
	'#9085e9',
	'#e66767'
];

/** The color of a group past the palette. */
export const otherColor = '#8f8f8f';

/**
 * A color for each group in a history's charts, the same for CPU and
 * memory, so a group keeps its color when you switch between them.
 */
export function seriesColors(h: UsageHistory): Map<string, string> {
	const colors = new Map<string, string>();
	for (const s of [...h.cpu, ...h.mem]) {
		if (!colors.has(s.key)) {
			colors.set(s.key, seriesPalette[colors.size] ?? otherColor);
		}
	}
	return colors;
}

/**
 * The top of an axis of bytes, in whole binary units, so its ticks read 1 GB
 * and 2 GB rather than 954 MB.
 */
export function bytesAxisMax(values: readonly (number | null)[]): number {
	const top = Math.max(0, ...values.map((v) => v ?? 0)) * 1.1;
	if (top <= 0) {
		return 1024 ** 3;
	}
	const unit = 1024 ** Math.floor(Math.log(top) / Math.log(1024));
	const n = [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024].find((n) => n * unit >= top) ?? 1024;
	return n * unit;
}

/** A moment, such as 2:15 PM today or Oct 17, 2:15 PM on another day. */
export function moment(ts: number, now = Date.now()): string {
	const d = new Date(ts);
	const today = d.toDateString() === new Date(now).toDateString();
	return d.toLocaleString(
		undefined,
		today
			? { hour: 'numeric', minute: '2-digit' }
			: { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' }
	);
}

/** The name in a group's key, such as nginx for container:nginx. */
export function groupName(key: string): string {
	const i = key.indexOf(':');
	return i < 0 ? key : key.slice(i + 1);
}
