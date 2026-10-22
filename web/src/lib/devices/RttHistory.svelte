<!--
	A device's round-trip times from the hub over the last hour, 6 hours, day
	or week, from the hub's pings. It loads again every minute.
-->
<script lang="ts">
	import { getDeviceHistory, type HistoryRange } from '#lib/api/client.js';
	import type { DeviceHistory } from '#lib/api/types.js';
	import AreaChart from '#lib/charts/AreaChart.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';

	interface Props {
		deviceId: string;
	}

	let { deviceId }: Props = $props();

	const ranges: { value: HistoryRange; label: string }[] = [
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

	let range: HistoryRange = $state('24h');
	let history: DeviceHistory | null = $state(null);
	let failed = $state(false);

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
</script>

<div>
	<div class="mb-2 flex flex-wrap items-center justify-between gap-2">
		<h3 class="text-[13px] font-medium text-fg-muted">Round-trip time from the hub</h3>
		<Segmented bind:value={range} options={ranges} label="Time range" />
	</div>
	{#if history}
		<div class="relative">
			<AreaChart
				label="Round-trip time from the hub over the last {rangeNames[range]}"
				series={[{ key: 'rtt', label: 'RTT', color: 'var(--color-amber)', points: history.rtt }]}
				from={Date.parse(history.from)}
				to={Date.parse(history.to)}
				format={(v) => `${v.toFixed(0)} ms`}
			/>
			{#if !history.rtt.some((p) => p.v !== null)}
				<p class="absolute inset-0 grid place-items-center pb-6 pl-14 text-[13px] text-fg-subtle">
					{history.rtt.length ? 'No ping got a reply' : 'No pings yet'} in the last {rangeNames[
						range
					]}.
				</p>
			{/if}
		</div>
	{:else if failed}
		<p class="grid h-40 place-items-center text-[13px] text-fg-subtle">
			Couldn't load the history. Roost tries again in a minute.
		</p>
	{:else}
		<Skeleton class="h-40" />
	{/if}
</div>
