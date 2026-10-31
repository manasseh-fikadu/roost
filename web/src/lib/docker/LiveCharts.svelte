<!--
	A container's last five minutes, a sample a second: its CPU, memory,
	network and disk. The container page owns the stream and passes the
	samples. Once the stream has ended, the charts stay as they were.
-->
<script lang="ts">
	import type { DockerStats } from '#lib/api/types.js';
	import type { ChartSeries } from '#lib/charts/AreaChart.svelte';
	import { bytes, percent, rate } from '#lib/format.js';
	import Card from '#lib/ui/Card.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import { cpuText } from '#lib/usage.js';
	import {
		bytesLabel,
		cpuAxisMax,
		liveWindow,
		memoryAxisMax,
		rateAxisMax,
		rateLabel,
		samplePoints,
		seriesColor,
		valuesOf
	} from './charts.js';
	import MetricChart from './MetricChart.svelte';

	interface Props {
		/** The latest samples, oldest first. */
		samples: DockerStats[];
		/** Why the stats ended, such as "The container stopped", or null while they go on. */
		ended: string | null;
		/** The container's memory limit in bytes, or null if it has none. */
		memLimit: number | null;
	}

	let { samples, ended, memLimit }: Props = $props();

	const last = $derived(samples.length > 0 ? samples[samples.length - 1] : null);
	// The window follows the clock of the samples, which Docker took, and not the browser's.
	const to = $derived(last?.ts);
	const from = $derived(to === undefined ? undefined : to - liveWindow);

	const cpu = $derived(samplePoints(samples, (s) => s.cpu));
	const mem = $derived(samplePoints(samples, (s) => s.memUsed));
	const rx = $derived(samplePoints(samples, (s) => s.netRx));
	const tx = $derived(samplePoints(samples, (s) => s.netTx));
	const read = $derived(samplePoints(samples, (s) => s.blkReadRate));
	const write = $derived(samplePoints(samples, (s) => s.blkWriteRate));

	function line(
		key: string,
		label: string,
		color: string,
		points: ChartSeries['points']
	): ChartSeries {
		return { key, label, color, points };
	}
</script>

<Card title="Live" description="A sample a second, for the last 5 minutes">
	{#if last}
		<div class="grid gap-6 p-4 lg:grid-cols-2">
			<MetricChart
				title="CPU"
				label="CPU over the last 5 minutes, in percent of one CPU"
				series={[line('cpu', 'CPU', seriesColor.cpu, cpu)]}
				format={cpuText}
				yMax={cpuAxisMax(valuesOf(cpu), last.cpus)}
				{from}
				{to}
				current={{ cpu: percent(last.cpu, 1) }}
				note={last.cpus > 1 ? `of ${last.cpus} CPUs` : undefined}
			/>
			<MetricChart
				title="Memory"
				label="Memory over the last 5 minutes"
				series={[line('mem', 'Memory', seriesColor.mem, mem)]}
				format={bytesLabel}
				yMax={memoryAxisMax(valuesOf(mem), memLimit)}
				{from}
				{to}
				current={{ mem: bytes(last.memUsed) }}
				note={memLimit ? `Limit ${bytes(memLimit)}` : undefined}
			/>
			<MetricChart
				title="Network"
				label="Network traffic over the last 5 minutes"
				series={[line('rx', 'In', seriesColor.rx, rx), line('tx', 'Out', seriesColor.tx, tx)]}
				format={rateLabel}
				yMax={rateAxisMax(valuesOf(rx, tx))}
				{from}
				{to}
				current={{ rx: rate(last.netRx), tx: rate(last.netTx) }}
			/>
			<MetricChart
				title="Disk I/O"
				label="Disk reads and writes over the last 5 minutes"
				series={[
					line('read', 'Read', seriesColor.read, read),
					line('write', 'Write', seriesColor.write, write)
				]}
				format={rateLabel}
				yMax={rateAxisMax(valuesOf(read, write))}
				{from}
				{to}
				current={{ read: rate(last.blkReadRate), write: rate(last.blkWriteRate) }}
			/>
		</div>
	{:else if !ended}
		<div class="grid gap-6 p-4 lg:grid-cols-2" aria-busy="true">
			{#each { length: 4 }, i (i)}
				<Skeleton class="h-[180px]" />
			{/each}
		</div>
	{/if}
	{#if ended}
		<p class={['px-4 py-2.5 text-[12.5px] text-fg-subtle', last && 'border-t border-line']}>
			Stats ended: {ended}
		</p>
	{/if}
</Card>
