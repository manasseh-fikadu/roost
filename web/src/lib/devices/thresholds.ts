// The thresholds an admin sets for one device in its Edit dialog. A number
// field holds NaN while it is empty, which means "use the setting".

import type { DeviceThresholds } from '#lib/api/types.js';

/** A device's own thresholds as a form holds them: NaN for each one that isn't set. */
export interface ThresholdDraft {
	diskWarn: number;
	diskCritical: number;
	memoryWarn: number;
	cpuWarn: number;
	offlineMinutes: number;
}

/** A form with no thresholds of its own. */
export function emptyDraft(): ThresholdDraft {
	return { diskWarn: NaN, diskCritical: NaN, memoryWarn: NaN, cpuWarn: NaN, offlineMinutes: NaN };
}

/** The form for a device's thresholds. */
export function draftOf(t: DeviceThresholds | undefined): ThresholdDraft {
	return {
		diskWarn: t?.diskWarn ?? NaN,
		diskCritical: t?.diskCritical ?? NaN,
		memoryWarn: t?.memoryWarn ?? NaN,
		cpuWarn: t?.cpuWarn ?? NaN,
		offlineMinutes: t?.offlineMinutes ?? NaN
	};
}

/** The thresholds to send for a form, leaving out the ones that aren't set. */
export function thresholdsOf(d: ThresholdDraft): DeviceThresholds {
	const out: DeviceThresholds = {};
	if (!Number.isNaN(d.diskWarn)) out.diskWarn = d.diskWarn;
	if (!Number.isNaN(d.diskCritical)) out.diskCritical = d.diskCritical;
	if (!Number.isNaN(d.memoryWarn)) out.memoryWarn = d.memoryWarn;
	if (!Number.isNaN(d.cpuWarn)) out.cpuWarn = d.cpuWarn;
	if (!Number.isNaN(d.offlineMinutes)) out.offlineMinutes = d.offlineMinutes;
	return out;
}

/** What is wrong with a form, in a sentence, or '' if nothing is. */
export function thresholdsProblem(d: ThresholdDraft): string {
	const percents: [string, number][] = [
		['disk warning', d.diskWarn],
		['critical disk', d.diskCritical],
		['memory', d.memoryWarn],
		['CPU', d.cpuWarn]
	];
	for (const [name, v] of percents) {
		// An empty field is NaN, and is fine.
		if (!Number.isNaN(v) && !(v > 0 && v <= 100)) {
			return `The ${name} threshold must be a percentage above 0.`;
		}
	}
	if (!Number.isNaN(d.diskWarn) && !Number.isNaN(d.diskCritical) && d.diskCritical < d.diskWarn) {
		return "A disk can't be critical before it needs a warning.";
	}
	const m = d.offlineMinutes;
	if (!Number.isNaN(m) && !(Number.isInteger(m) && m >= 1 && m <= 7 * 24 * 60)) {
		return 'The time a device can be offline must be from a minute to a week.';
	}
	return '';
}
