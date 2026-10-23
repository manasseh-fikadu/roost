// The time ranges of a device's history.

import type { HistoryRange } from './api/client.js';
import type { Point } from './api/types.js';

export type { HistoryRange };

/** The ranges, for a picker. */
export const historyRanges: { value: HistoryRange; label: string }[] = [
	{ value: '1h', label: '1h' },
	{ value: '6h', label: '6h' },
	{ value: '24h', label: '24h' },
	{ value: '7d', label: '7d' }
];

const rangeNames: Record<HistoryRange, string> = {
	'1h': 'hour',
	'6h': '6 hours',
	'24h': 'day',
	'7d': 'week'
};

/** A range for "over the last …", such as "hour" or "6 hours". */
export function rangeName(range: HistoryRange): string {
	return rangeNames[range];
}

/** A history's span, as the hub sends it: RFC 3339 times, and a step in milliseconds. */
interface Span {
	from: string;
	to: string;
	step: number;
}

/**
 * The value of each step of a history, from its first step to its last,
 * or null for a step with no point, so they can be drawn evenly spaced. The
 * hub leaves out the steps with no data.
 */
export function stepValues(span: Span, points: Point[]): (number | null)[] {
	const byStep = new Map(points.map((p) => [p.ts, p.v]));
	const out: (number | null)[] = [];
	for (let ts = Date.parse(span.from); ts <= Date.parse(span.to); ts += span.step) {
		out.push(byStep.get(ts) ?? null);
	}
	return out;
}
