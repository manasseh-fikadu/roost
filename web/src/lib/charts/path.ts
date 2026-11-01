// Geometry shared by the charts.

/** A value at a time, in Unix milliseconds. The value is null where there's no data. */
export interface Point {
	ts: number;
	v: number | null;
}

/** A point on the drawing, in pixels. */
type XY = [x: number, y: number];

/**
 * Splits points into the runs a line joins. A null value or a gap longer
 * than maxGap between two points ends a run, so the line breaks there
 * instead of drawing over missing data.
 */
export function runs(
	points: Point[],
	x: (ts: number) => number,
	y: (v: number) => number,
	maxGap: number
): XY[][] {
	const out: XY[][] = [];
	let run: XY[] = [];
	let prevTs: number | null = null;
	for (const p of points) {
		if (p.v === null || (prevTs !== null && p.ts - prevTs > maxGap)) {
			if (run.length) {
				out.push(run);
			}
			run = [];
		}
		if (p.v === null) {
			prevTs = null;
			continue;
		}
		run.push([x(p.ts), y(p.v)]);
		prevTs = p.ts;
	}
	if (run.length) {
		out.push(run);
	}
	return out;
}

/** The SVG path of a line through run. */
export function linePath(run: XY[]): string {
	return run.map(([x, y], i) => `${i ? 'L' : 'M'}${x.toFixed(1)},${y.toFixed(1)}`).join('');
}

/** The SVG path of the area between a line through run and the baseline y. */
export function areaPath(run: XY[], baseline: number): string {
	const first = run[0][0].toFixed(1);
	const last = run[run.length - 1][0].toFixed(1);
	return `${linePath(run)}L${last},${baseline}L${first},${baseline}Z`;
}

/** The usual time between points: the median of the first 50 gaps. */
export function typicalStep(points: Point[]): number {
	const gaps: number[] = [];
	for (let i = 1; i < Math.min(points.length, 50); i++) {
		gaps.push(points[i].ts - points[i - 1].ts);
	}
	gaps.sort((a, b) => a - b);
	return gaps[Math.floor(gaps.length / 2)] || 60_000;
}

/** A round number at or above v for the top of an axis: 1, 2, 2.5 or 5 times a power of ten. */
export function niceMax(v: number): number {
	if (v <= 0) {
		return 1;
	}
	const exp = 10 ** Math.floor(Math.log10(v));
	const f = v / exp;
	const nice = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10;
	return nice * exp;
}

/** The index of the value in sorted that's nearest to target. sorted must not be empty. */
export function nearest(sorted: number[], target: number): number {
	let lo = 0;
	let hi = sorted.length - 1;
	while (lo < hi) {
		const mid = (lo + hi) >> 1;
		if (sorted[mid] < target) {
			lo = mid + 1;
		} else {
			hi = mid;
		}
	}
	return lo > 0 && target - sorted[lo - 1] < sorted[lo] - target ? lo - 1 : lo;
}

/**
 * How to write the times on a chart's axis, for a chart that spans `span`
 * milliseconds: the time of day for a day or two, the weekday and hour for up
 * to two weeks, and the date past that, when there are several of each weekday.
 */
export function timeTickFormat(span: number): Intl.DateTimeFormatOptions {
	const day = 86_400_000;
	if (span > 14 * day) {
		return { month: 'short', day: 'numeric' };
	}
	if (span > 2 * day) {
		return { weekday: 'short', hour: 'numeric' };
	}
	return { hour: 'numeric', minute: '2-digit' };
}
