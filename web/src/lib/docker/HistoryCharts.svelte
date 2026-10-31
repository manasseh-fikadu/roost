<!--
	What a container used over the last hour, 6 hours, day or week, from the
	history Roost keeps by its name, which outlasts the container being made
	again. It loads again every minute.
-->
<script lang="ts">
	import { getDockerHistory } from './api.js';
	import type { DockerHistory } from '#lib/api/types.js';
	import type { ChartSeries } from '#lib/charts/AreaChart.svelte';
	import { bytes } from '#lib/format.js';
	import { historyRanges, rangeName, type HistoryRange } from '#lib/history.js';
	import Card from '#lib/ui/Card.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import { cpuText } from '#lib/usage.js';
	import {
		bytesLabel,
		hasData,
		memoryAxisMax,
		rateAxisMax,
		rateLabel,
		seriesColor,
		stepPoints,
		valuesOf
	} from './charts.js';
	import MetricChart from './MetricChart.svelte';

	interface Props {
		deviceId: string;
		/** The container's name, which its history is kept by. */
		container: string;
		/** The container's memory limit in bytes, or null if it has none. */
		memLimit: number | null;
	}

	let { deviceId, container, memLimit }: Props = $props();

	let range = $state<HistoryRange>('1h');
	let history = $state.raw<DockerHistory | null>(null);
	let failed = $state(false);

	// Only the IDs and the range matter, so a new `memLimit` doesn't load it all again.
	$effect(() => {
		const id = deviceId;
		const name = container;
		const r = range;
		let current = true;
		history = null;
		failed = false;
		async function load() {
			try {
				const h = await getDockerHistory(id, name, r);
				if (current) {
					history = h;
					failed = false;
				}
			} catch {
				if (current) {
					failed = true;
				}
			}
		}
		load();
		const timer = setInterval(load, 60_000);
		return () => {
			current = false;
			clearInterval(timer);
		};
	});

	function line(
		key: string,
		label: string,
		color: string,
		points: ChartSeries['points']
	): ChartSeries {
		return { key, label, color, points };
	}

	/** The history's series with a point at every step, so a quiet stretch is a gap. */
	const series = $derived.by(() => {
		const h = history;
		if (!h) {
			return null;
		}
		const at = (points: DockerHistory['cpu']) => stepPoints(h, points);
		return {
			cpu: at(h.cpu),
			mem: at(h.mem),
			rx: at(h.netRx),
			tx: at(h.netTx),
			read: at(h.blkRead),
			write: at(h.blkWrite)
		};
	});

	const empty = $derived(
		series !== null &&
			!hasData(series.cpu, series.mem, series.rx, series.tx, series.read, series.write)
	);
	const over = $derived(`over the last ${rangeName(range)}`);
</script>

<Card title="History" description="Averages of each minute, kept by the container's name">
	{#snippet actions()}
		<Segmented bind:value={range} options={historyRanges} label="Time range" />
	{/snippet}
	{#if history && series}
		{#if empty}
			<p class="px-4 py-10 text-center text-[13px] text-fg-subtle">
				Roost records each container once a minute, so history builds up from when it first sees it.
			</p>
		{:else}
			<div class="grid gap-6 p-4 lg:grid-cols-2">
				<MetricChart
					title="CPU"
					label="CPU {over}, in percent of one CPU"
					series={[line('cpu', 'CPU', seriesColor.cpu, series.cpu)]}
					format={cpuText}
					from={Date.parse(history.from)}
					to={Date.parse(history.to)}
				/>
				<MetricChart
					title="Memory"
					label="Memory {over}"
					series={[line('mem', 'Memory', seriesColor.mem, series.mem)]}
					format={bytesLabel}
					yMax={memoryAxisMax(valuesOf(series.mem), memLimit)}
					from={Date.parse(history.from)}
					to={Date.parse(history.to)}
					note={memLimit ? `Limit ${bytes(memLimit)}` : undefined}
				/>
				<MetricChart
					title="Network"
					label="Network traffic {over}"
					series={[
						line('rx', 'In', seriesColor.rx, series.rx),
						line('tx', 'Out', seriesColor.tx, series.tx)
					]}
					format={rateLabel}
					yMax={rateAxisMax(valuesOf(series.rx, series.tx))}
					from={Date.parse(history.from)}
					to={Date.parse(history.to)}
				/>
				<MetricChart
					title="Disk I/O"
					label="Disk reads and writes {over}"
					series={[
						line('read', 'Read', seriesColor.read, series.read),
						line('write', 'Write', seriesColor.write, series.write)
					]}
					format={rateLabel}
					yMax={rateAxisMax(valuesOf(series.read, series.write))}
					from={Date.parse(history.from)}
					to={Date.parse(history.to)}
				/>
			</div>
		{/if}
	{:else if failed}
		<p class="px-4 py-10 text-center text-[13px] text-fg-subtle">
			Couldn't load the history. Roost tries again in a minute.
		</p>
	{:else}
		<div class="grid gap-6 p-4 lg:grid-cols-2" aria-busy="true">
			{#each { length: 4 }, i (i)}
				<Skeleton class="h-[180px]" />
			{/each}
		</div>
	{/if}
</Card>
