// What the Docker charts draw: the points of each series, and the top of each axis.

import type { DockerStats } from '#lib/api/types.js';
import { niceMax, type Point } from '#lib/charts/path.js';
import { bytes, rateShort } from '#lib/format.js';
import { stepValues } from '#lib/history.js';

/** The color of each series, as the Docker pages draw it. */
export const seriesColor = {
	cpu: 'var(--color-blue-fg)',
	mem: 'var(--color-purple-fg)',
	rx: 'var(--color-green-fg)',
	tx: 'var(--color-fg)',
	read: 'var(--color-blue-fg)',
	write: 'var(--color-amber)'
};

/** The live charts show the last five minutes, which is as many samples as the hub keeps. */
export const liveWindow = 300_000;

/** The points of one value of each sample. A value that is missing or isn't a number leaves a gap. */
export function samplePoints(
	samples: readonly DockerStats[],
	pick: (s: DockerStats) => number | null
): Point[] {
	return samples.map((s) => {
		const v = pick(s);
		return { ts: s.ts, v: v !== null && Number.isFinite(v) ? v : null };
	});
}

/**
 * A history's points at every step of its range, with a gap where the hub
 * had none, so a quiet stretch is drawn as a gap and not joined across. It
 * leaves the points alone if their step makes no sense.
 */
export function stepPoints(
	span: { from: string; to: string; step: number },
	points: Point[]
): Point[] {
	const from = Date.parse(span.from);
	const steps = (Date.parse(span.to) - from) / span.step;
	if (!(span.step > 0) || !Number.isFinite(steps) || steps > 20_000) {
		return points;
	}
	return stepValues(span, points).map((v, i) => ({ ts: from + i * span.step, v }));
}

/** Whether any of the series has a value. */
export function hasData(...series: Point[][]): boolean {
	return series.some((points) => points.some((p) => p.v !== null));
}

function peak(values: readonly (number | null)[]): number {
	let top = 0;
	for (const v of values) {
		if (v !== null && Number.isFinite(v) && v > top) {
			top = v;
		}
	}
	return top;
}

/** The values of series, for working out an axis. */
export function valuesOf(...series: Point[][]): (number | null)[] {
	return series.flatMap((points) => points.map((p) => p.v));
}

/**
 * The top of the CPU axis, in percent of one CPU. It is never under one CPU,
 * so a container that uses a sliver of one doesn't fill the chart. It can't
 * be over what all the container's CPUs make, which is as much as it can
 * use, and otherwise it is a round number above the highest value.
 */
export function cpuAxisMax(values: readonly (number | null)[], cpus: number): number {
	const nice = niceMax(peak(values) * 1.1);
	const cap = cpus > 0 ? cpus * 100 : Infinity;
	return Math.max(100, Math.min(nice, cap));
}

/**
 * The top of an axis of bytes, in whole binary units, so its ticks read 1 MB
 * and 2 MB rather than 954 KB. It is at least `floor`.
 */
export function binaryAxisMax(values: readonly (number | null)[], floor: number): number {
	const top = peak(values) * 1.1;
	if (top <= floor) {
		return floor;
	}
	const unit = 1024 ** Math.floor(Math.log(top) / Math.log(1024));
	const n = [1, 2, 4, 8, 16, 32, 64, 128, 256, 512, 1024].find((n) => n * unit >= top) ?? 1024;
	return Math.max(floor, n * unit);
}

/** The top of an axis of bytes per second: at least 1 KB/s, so an idle container isn't drawn on a scale of bytes. */
export function rateAxisMax(values: readonly (number | null)[]): number {
	return binaryAxisMax(values, 1024);
}

/**
 * How many times its usage a memory limit may be before the chart stops
 * using it as the top of the axis. A container with no limit is told it has
 * all the machine's memory, and a chart that high would draw a flat line.
 */
const absurdLimit = 20;

/**
 * The top of the memory axis: the container's limit, so the chart shows how
 * close it is, unless the limit is so far above what it uses that the
 * chart would show nothing.
 */
export function memoryAxisMax(values: readonly (number | null)[], limit: number | null): number {
	if (limit !== null && limit > 0 && peak(values) * absurdLimit >= limit) {
		return limit;
	}
	return binaryAxisMax(values, 1024 ** 2);
}

/** Bytes on an axis, in a few characters, such as 512MB. */
export function bytesLabel(v: number): string {
	return bytes(v, 0).replace(' ', '');
}

/** Bytes per second on an axis, such as 1.0K/s. */
export function rateLabel(v: number): string {
	return `${rateShort(v)}/s`;
}
