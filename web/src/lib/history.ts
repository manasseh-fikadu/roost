// The time ranges of a device's history.

import type { HistoryRange } from './api/client.js';

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
