<!--
	One chart of the Docker pages: a title, a legend that can say what each
	series is now, and the chart.
-->
<script lang="ts">
	import AreaChart, { type ChartSeries } from '#lib/charts/AreaChart.svelte';

	interface Props {
		title: string;
		/** What the chart shows, for screen readers, such as "CPU over the last 5 minutes". */
		label: string;
		series: ChartSeries[];
		format: (v: number) => string;
		/** The value at the top of the axis. */
		yMax?: number;
		/** The time at the left and right edges, in Unix milliseconds. */
		from?: number;
		to?: number;
		height?: number;
		/** What each series is now, by its key, to show in the legend. */
		current?: Record<string, string>;
		/** More for the legend, such as a limit. */
		note?: string;
		/** Words over the chart, such as when there is nothing to draw. */
		empty?: string;
	}

	let {
		title,
		label,
		series,
		format,
		yMax,
		from,
		to,
		height = 150,
		current,
		note,
		empty
	}: Props = $props();
</script>

<div class="min-w-0">
	<div class="mb-2 flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
		<h3 class="text-[13px] font-medium text-fg-muted">{title}</h3>
		<ul class="flex flex-wrap items-center gap-x-4 gap-y-1">
			{#each series as s (s.key)}
				<li class="flex items-center gap-1.5 text-[12px] text-fg-muted">
					<span class="size-2 shrink-0 rounded-sm" style:background={s.color}></span>
					{s.label}
					{#if current}
						<span class="num text-fg">{current[s.key] ?? '—'}</span>
					{/if}
				</li>
			{/each}
			{#if note}
				<li class="num text-[12px] text-fg-subtle">{note}</li>
			{/if}
		</ul>
	</div>
	<div class="relative">
		<AreaChart {label} {series} {format} {yMax} {from} {to} {height} />
		{#if empty}
			<p class="absolute inset-0 grid place-items-center pb-6 pl-14 text-[13px] text-fg-subtle">
				{empty}
			</p>
		{/if}
	</div>
</div>
