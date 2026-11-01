<!--
	A time-series chart of one or more lines, each with a fading area under it.
	Pointing at it, or focusing it and pressing the arrow keys, shows every
	series' value at that time.
-->
<script lang="ts" module>
	import type { Point } from './path.js';

	export interface ChartSeries {
		key: string;
		label: string;
		/** A CSS color. */
		color: string;
		/** The series' points, oldest first. */
		points: Point[];
	}
</script>

<script lang="ts">
	import { areaPath, linePath, nearest, niceMax, runs, typicalStep } from './path.js';

	interface Props {
		series: ChartSeries[];
		/** What the chart shows, for screen readers, such as "CPU over the last hour". */
		label: string;
		height?: number;
		/** The value at the top of the axis. A round number above the largest value if it's not given. */
		yMax?: number;
		/** Formats a value for the axis and the tooltip. */
		format?: (v: number) => string;
		/** The time at the left edge, in Unix milliseconds. The first point's if it's not given. */
		from?: number;
		/** The time at the right edge. The last point's if it's not given. */
		to?: number;
		/** Makes points selectable, with a click or Enter. */
		onSelect?: (ts: number) => void;
		/** Areas under the lines. Turn them off when several series overlap. */
		fill?: boolean;
	}

	let {
		series,
		label,
		height = 160,
		yMax,
		format = (v: number) => v.toFixed(0),
		from,
		to,
		onSelect,
		fill = true
	}: Props = $props();

	const pad = { left: 56, right: 8, top: 8, bottom: 22 };
	const gradient = $props.id();

	let width = $state(0);
	/** The time being pointed at, or null. */
	let hover: number | null = $state(null);

	const all = $derived(series.flatMap((s) => s.points));
	const t0 = $derived(
		from ?? (all.length ? Math.min(...all.map((p) => p.ts)) : Date.now() - 3_600_000)
	);
	const t1 = $derived(to ?? (all.length ? Math.max(...all.map((p) => p.ts)) : Date.now()));
	const top = $derived(
		yMax !== undefined && yMax > 0 ? yMax : niceMax(Math.max(0, ...all.map((p) => p.v ?? 0)) * 1.1)
	);
	const innerW = $derived(Math.max(0, width - pad.left - pad.right));
	const innerH = $derived(height - pad.top - pad.bottom);
	const baseline = $derived(pad.top + innerH);
	const step = $derived(typicalStep(series[0]?.points ?? []));
	const times = $derived([...new Set(all.map((p) => p.ts))].sort((a, b) => a - b));

	function x(ts: number): number {
		return pad.left + (t1 > t0 ? ((ts - t0) / (t1 - t0)) * innerW : innerW / 2);
	}

	function y(v: number): number {
		return pad.top + innerH - (Math.min(v, top) / top) * innerH;
	}

	const yTicks = $derived([0, 0.5, 1].map((f) => f * top));
	const xTicks = $derived.by(() => {
		if (width <= 0) {
			return [];
		}
		// All the points at one time: one label in the middle, not several on top of each other.
		if (t1 <= t0) {
			return [t0];
		}
		const count = width > 500 ? 5 : 3;
		return Array.from({ length: count }, (_, i) => t0 + ((t1 - t0) * i) / (count - 1));
	});
	const tickFormat: Intl.DateTimeFormatOptions = $derived(
		t1 - t0 > 2 * 86_400_000
			? { weekday: 'short', hour: 'numeric' }
			: { hour: 'numeric', minute: '2-digit' }
	);

	function valueAt(s: ChartSeries, ts: number): number | null {
		return s.points.find((p) => p.ts === ts)?.v ?? null;
	}

	function when(ts: number): string {
		return new Date(ts).toLocaleString(undefined, {
			month: 'short',
			day: 'numeric',
			hour: 'numeric',
			minute: '2-digit'
		});
	}

	/** The time of the point nearest the pointer. */
	function timeAt(e: MouseEvent): number | null {
		if (!times.length || innerW <= 0) {
			return null;
		}
		const rect = (e.currentTarget as Element).getBoundingClientRect();
		const ts = t0 + ((e.clientX - rect.left - pad.left) / innerW) * (t1 - t0);
		return times[nearest(times, ts)];
	}

	function onkeydown(e: KeyboardEvent) {
		if (!times.length) {
			return;
		}
		const i = hover === null ? times.length - 1 : nearest(times, hover);
		switch (e.key) {
			case 'ArrowLeft':
				hover = times[Math.max(0, i - 1)];
				break;
			case 'ArrowRight':
				hover = times[Math.min(times.length - 1, i + 1)];
				break;
			case 'Home':
				hover = times[0];
				break;
			case 'End':
				hover = times[times.length - 1];
				break;
			case 'Enter':
			case ' ':
				if (hover !== null && onSelect) {
					onSelect(hover);
				}
				break;
			default:
				return;
		}
		e.preventDefault();
	}

	/** The tooltip's text, for screen readers. */
	const valueText = $derived(
		hover === null
			? undefined
			: `${when(hover)}: ${series
					.map((s) => {
						const v = valueAt(s, hover!);
						return `${s.label} ${v === null ? 'no data' : format(v)}`;
					})
					.join(', ')}`
	);
</script>

<div bind:clientWidth={width} class="relative w-full select-none" style:height="{height}px">
	{#if width > 0}
		<svg
			{width}
			{height}
			class={['block touch-pan-y rounded-sm', onSelect && 'cursor-pointer']}
			role="slider"
			tabindex="0"
			aria-label={label}
			aria-valuemin={times[0]}
			aria-valuemax={times[times.length - 1]}
			aria-valuenow={hover ?? undefined}
			aria-valuetext={valueText}
			onpointermove={(e) => (hover = timeAt(e))}
			onpointerleave={() => (hover = null)}
			onfocus={() => (hover ??= times[times.length - 1] ?? null)}
			onblur={() => (hover = null)}
			{onkeydown}
			onclick={(e) => {
				// From the click itself: on touch screens, pointerleave clears the
				// hover before the click arrives.
				const ts = timeAt(e);
				if (ts !== null && onSelect) {
					onSelect(ts);
				}
			}}
		>
			<defs>
				{#each series as s, i (s.key)}
					<linearGradient id="{gradient}-{i}" x1="0" x2="0" y1="0" y2="1">
						<stop offset="0%" stop-color={s.color} stop-opacity="0.2" />
						<stop offset="100%" stop-color={s.color} stop-opacity="0" />
					</linearGradient>
				{/each}
			</defs>
			{#each yTicks as t, i (i)}
				<line
					x1={pad.left}
					x2={width - pad.right}
					y1={y(t)}
					y2={y(t)}
					stroke="var(--color-line)"
					stroke-dasharray={t === 0 ? undefined : '3 3'}
				/>
				<text
					x={pad.left - 8}
					y={y(t) + 3}
					text-anchor="end"
					class="fill-fg-faint font-mono text-[10px]"
				>
					{format(t)}
				</text>
			{/each}
			{#each xTicks as t, i (i)}
				<text
					x={x(t)}
					y={height - 6}
					text-anchor={xTicks.length === 1
						? 'middle'
						: i === 0
							? 'start'
							: i === xTicks.length - 1
								? 'end'
								: 'middle'}
					class="fill-fg-faint font-mono text-[10px]"
				>
					{new Date(t).toLocaleString(undefined, tickFormat)}
				</text>
			{/each}
			{#each series as s, i (s.key)}
				{#each runs(s.points, x, y, step * 3) as run, j (j)}
					{#if run.length === 1}
						<!-- A point between two gaps has no line, so it gets a dot. -->
						<circle cx={run[0][0]} cy={run[0][1]} r="2" fill={s.color} />
					{:else}
						{#if fill}
							<path d={areaPath(run, baseline)} fill="url(#{gradient}-{i})" />
						{/if}
						<path
							d={linePath(run)}
							fill="none"
							stroke={s.color}
							stroke-width="1.5"
							stroke-linejoin="round"
						/>
					{/if}
				{/each}
			{/each}
			{#if hover !== null}
				<line
					x1={x(hover)}
					x2={x(hover)}
					y1={pad.top}
					y2={baseline}
					stroke="var(--color-line-heavy)"
				/>
				{#each series as s (s.key)}
					{@const v = valueAt(s, hover)}
					{#if v !== null}
						<circle
							cx={x(hover)}
							cy={y(v)}
							r="3"
							fill={s.color}
							stroke="var(--color-canvas)"
							stroke-width="1.5"
						/>
					{/if}
				{/each}
			{/if}
		</svg>
		{#if hover !== null}
			<div
				class="pointer-events-none absolute top-1 z-10 min-w-36 rounded-md border border-line-strong bg-surface/95 px-2.5 py-2 shadow-xl backdrop-blur"
				style:left={x(hover) <= width / 2 ? `${x(hover) + 10}px` : undefined}
				style:right={x(hover) > width / 2 ? `${width - x(hover) + 10}px` : undefined}
				aria-hidden="true"
			>
				<div class="mb-1 font-mono text-[10.5px] text-fg-subtle">{when(hover)}</div>
				{#each series as s (s.key)}
					{@const v = valueAt(s, hover)}
					<div class="flex items-center justify-between gap-4 text-[12px]">
						<span class="flex items-center gap-1.5 text-fg-muted">
							<span class="size-2 rounded-sm" style:background={s.color}></span>
							{s.label}
						</span>
						<span class="num text-fg">{v === null ? '—' : format(v)}</span>
					</div>
				{/each}
			</div>
		{/if}
	{/if}
</div>
