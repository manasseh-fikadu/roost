<!--
	A device's history over the last hour, 6 hours, day or week: its CPU and
	memory, its network, and its round-trip time from the hub. Each chart shows
	up if the device has that kind of history. It loads again every minute.
-->
<script lang="ts">
	import { getDeviceHistory } from '#lib/api/client.js';
	import type { Device, DeviceHistory } from '#lib/api/types.js';
	import AreaChart, { type ChartSeries } from '#lib/charts/AreaChart.svelte';
	import type { Point } from '#lib/charts/path.js';
	import { rateShort } from '#lib/format.js';
	import { rangeName, type HistoryRange } from '#lib/history.js';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Props {
		device: Device;
		range: HistoryRange;
		/**
		 * Makes the CPU and memory chart clickable. It's called with the
		 * middle of the step that was clicked, in Unix milliseconds.
		 */
		onSelect?: (ts: number) => void;
	}

	let { device, range, onSelect }: Props = $props();

	interface Chart {
		title: string;
		/** What the chart shows, for screen readers. */
		label: string;
		series: ChartSeries[];
		format: (v: number) => string;
		yMax?: number;
		/** What the chart says when it has nothing to show. */
		empty: string;
		/** A line next to the title. */
		hint?: string;
		select?: (ts: number) => void;
	}

	let history = $state<DeviceHistory | null>(null);
	let failed = $state(false);
	// Every snapshot passes in a new `device`, so the effect reads only its
	// ID, and loads again only for another device or range.
	const deviceId = $derived(device.id);

	$effect(() => {
		const id = deviceId;
		const r = range;
		let current = true;
		history = null;
		failed = false;
		async function load() {
			try {
				const h = await getDeviceHistory(id, r);
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

	function hasData(...series: Point[][]): boolean {
		return series.some((points) => points.some((p) => p.v !== null));
	}

	const charts: Chart[] = $derived.by(() => {
		const h = history;
		if (!h) {
			return [];
		}
		const over = `over the last ${rangeName(range)}`;
		const out: Chart[] = [];
		// A device Bield doesn't measure may still have history from before.
		if (device.measuredBy || hasData(h.cpu, h.memory, h.netRx, h.netTx)) {
			const empty = `Nothing measured in the last ${rangeName(range)}.`;
			out.push({
				title: 'CPU and memory',
				label: `CPU and memory ${over}`,
				series: [
					{ key: 'cpu', label: 'CPU', color: 'var(--color-blue-fg)', points: h.cpu },
					{ key: 'mem', label: 'Memory', color: 'var(--color-purple-fg)', points: h.memory }
				],
				format: (v) => `${v.toFixed(0)}%`,
				yMax: 100,
				empty: hasData(h.cpu, h.memory) ? '' : empty,
				hint: onSelect ? 'Click a point to see what was running' : undefined,
				select: onSelect && ((ts) => onSelect(ts + h.step / 2))
			});
			out.push({
				title: 'Network',
				label: `Network traffic ${over}`,
				series: [
					{ key: 'rx', label: 'Download', color: 'var(--color-green-fg)', points: h.netRx },
					{ key: 'tx', label: 'Upload', color: 'var(--color-fg)', points: h.netTx }
				],
				format: (v) => `${rateShort(v)}/s`,
				empty: hasData(h.netRx, h.netTx) ? '' : empty
			});
		}
		if (!device.self) {
			out.push({
				title: 'Round-trip time from the hub',
				label: `Round-trip time from the hub ${over}`,
				series: [{ key: 'rtt', label: 'RTT', color: 'var(--color-amber)', points: h.rtt }],
				format: (v) => `${v.toFixed(0)} ms`,
				empty: hasData(h.rtt)
					? ''
					: `${h.rtt.length ? 'No ping got a reply' : 'No pings yet'} in the last ${rangeName(range)}.`
			});
		}
		return out;
	});
</script>

{#if history}
	<div class="space-y-6">
		{#each charts as c (c.title)}
			<div>
				<div class="mb-2 flex items-baseline justify-between gap-3">
					<h3 class="text-[13px] font-medium text-fg-muted">{c.title}</h3>
					{#if c.hint && !c.empty}
						<span class="hidden text-[12px] text-fg-faint sm:inline">{c.hint}</span>
					{/if}
				</div>
				<div class="relative">
					<AreaChart
						label={c.label}
						series={c.series}
						from={Date.parse(history.from)}
						to={Date.parse(history.to)}
						format={c.format}
						yMax={c.yMax}
						onSelect={c.select}
					/>
					{#if c.empty}
						<p
							class="absolute inset-0 grid place-items-center pb-6 pl-14 text-[13px] text-fg-subtle"
						>
							{c.empty}
						</p>
					{/if}
				</div>
			</div>
		{/each}
	</div>
{:else if failed}
	<p class="grid h-40 place-items-center text-[13px] text-fg-subtle">
		Couldn't load the history. Bield tries again in a minute.
	</p>
{:else}
	<Skeleton class="h-40" />
{/if}
