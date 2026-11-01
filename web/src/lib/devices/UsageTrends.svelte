<!--
	Which groups of processes used a device's CPU or memory over a range: a
	chart of the biggest few, which takes a click to show that moment, and a
	table of the biggest ones' averages and peaks.
-->
<script lang="ts">
	import type { UsageHistory } from '#lib/api/types.js';
	import AreaChart from '#lib/charts/AreaChart.svelte';
	import { bytes } from '#lib/format.js';
	import { historyRanges, type HistoryRange } from '#lib/history.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import {
		bytesAxisMax,
		cpuText,
		kindLabel,
		kindTone,
		otherColor,
		seriesColors,
		usageValue,
		type UsageMetric
	} from '#lib/usage.js';

	interface Props {
		/** The history, or null while it loads. */
		history: UsageHistory | null;
		metric: UsageMetric;
		range: HistoryRange;
		/** Called with the time of the middle of a step that was clicked. */
		onSelect: (ts: number) => void;
	}

	let { history, metric, range = $bindable(), onSelect }: Props = $props();

	const colors = $derived(history ? seriesColors(history) : new Map<string, string>());
	const series = $derived(history ? (metric === 'cpu' ? history.cpu : history.mem) : []);
	const consumers = $derived.by(() => {
		if (!history) {
			return [];
		}
		const avg = (c: (typeof history.consumers)[number]) => (metric === 'cpu' ? c.cpuAvg : c.memAvg);
		return history.consumers
			.filter((c) => avg(c) > 0)
			.sort((a, b) => avg(b) - avg(a))
			.slice(0, 10);
	});
	const cols =
		'grid grid-cols-[minmax(0,1fr)_64px_64px] gap-x-3 sm:grid-cols-[minmax(0,1fr)_80px_80px_96px]';
</script>

<section class="mt-6">
	<div class="mb-3 flex flex-wrap items-center justify-between gap-2">
		<div>
			<h3 class="text-[14px] font-medium text-fg">Top consumers over time</h3>
			<p class="text-[12px] text-fg-subtle">
				One-minute averages. Click the chart to see that moment.
				{#if range === '30d' || range === '90d'}
					Bield keeps them for 7 days, so the older part is empty.
				{/if}
			</p>
		</div>
		<Segmented bind:value={range} options={historyRanges} label="Time range" />
	</div>
	{#if !history}
		<Skeleton class="h-56" />
	{:else if history.minutes === 0}
		<p
			class="rounded-lg border border-line bg-canvas px-4 py-6 text-center text-[13px] text-fg-subtle"
		>
			Nothing recorded in this range yet. Bield stores a summary every minute.
		</p>
	{:else}
		<ul class="mb-2 flex flex-wrap gap-x-4 gap-y-1">
			{#each series as s (s.key)}
				<li class="flex min-w-0 items-center gap-1.5 text-[12px] text-fg-muted">
					<span class="size-2 shrink-0 rounded-sm" style:background={colors.get(s.key)}></span>
					<span class="max-w-40 truncate">{s.name}</span>
				</li>
			{/each}
		</ul>
		<AreaChart
			label="{metric === 'cpu' ? 'CPU' : 'Memory'} of the biggest groups of processes"
			from={Date.parse(history.from)}
			to={Date.parse(history.to)}
			height={180}
			fill={false}
			yMax={metric === 'mem'
				? bytesAxisMax(series.flatMap((s) => s.points.map((p) => p.v)))
				: undefined}
			format={(v) => (metric === 'cpu' ? cpuText(v) : bytes(v, 0))}
			series={series.map((s) => ({
				key: s.key,
				label: s.name,
				color: colors.get(s.key) ?? otherColor,
				points: s.points
			}))}
			onSelect={(ts) => onSelect(ts + history.step / 2)}
		/>
		<div class="mt-4 overflow-hidden rounded-lg border border-line">
			<div class={[cols, 'border-b border-line bg-canvas/50 px-3 py-2 text-[12px] text-fg-subtle']}>
				<span>Group</span>
				<span class="text-right">Average</span>
				<span class="text-right">Peak</span>
				<span class="hidden text-right sm:block">{metric === 'cpu' ? 'Avg memory' : 'Avg CPU'}</span
				>
			</div>
			{#each consumers as c (c.key)}
				<div class={[cols, 'items-center border-b border-line px-3 py-2 last:border-0']}>
					<span class="flex min-w-0 items-center gap-2">
						<span
							class="size-2 shrink-0 rounded-sm"
							style:background={colors.get(c.key) ?? 'transparent'}
						></span>
						<span class="truncate text-[13px] text-fg" title={c.name}>{c.name}</span>
						<Badge tone={kindTone(c.kind)} class="hidden sm:inline-flex">{kindLabel(c.kind)}</Badge>
					</span>
					<span class="text-right num text-[12.5px] text-fg">
						{usageValue(metric, metric === 'cpu' ? c.cpuAvg : c.memAvg)}
					</span>
					<span class="text-right num text-[12.5px] text-fg-muted">
						{usageValue(metric, metric === 'cpu' ? c.cpuPeak : c.memPeak)}
					</span>
					<span class="hidden text-right num text-[12.5px] text-fg-subtle sm:block">
						{metric === 'cpu' ? bytes(c.memAvg) : cpuText(c.cpuAvg)}
					</span>
				</div>
			{/each}
		</div>
	{/if}
</section>
