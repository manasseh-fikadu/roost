<!--
	A check's latency and uptime over the last hour, 6 hours, day or week. It
	loads again every minute.
-->
<script lang="ts">
	import { getCheckHistory } from '#lib/api/client.js';
	import type { Check, CheckHistory } from '#lib/api/types.js';
	import AreaChart from '#lib/charts/AreaChart.svelte';
	import { latency } from '#lib/format.js';
	import { rangeName, type HistoryRange } from '#lib/history.js';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Props {
		check: Check;
		range: HistoryRange;
	}

	let { check, range }: Props = $props();

	let history = $state.raw<CheckHistory | null>(null);
	let failed = $state(false);
	// Every snapshot passes in a new `check`, so the effect reads only its
	// ID, and loads again only for another check or range.
	const checkId = $derived(check.id);

	$effect(() => {
		const id = checkId;
		const r = range;
		let current = true;
		history = null;
		failed = false;
		async function load() {
			try {
				const h = await getCheckHistory(id, r);
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

	const over = $derived(`over the last ${rangeName(range)}`);
	const empty = $derived(
		history && history.uptime.length === 0 ? `It didn't run in the last ${rangeName(range)}.` : ''
	);
</script>

{#snippet chart(
	title: string,
	label: string,
	series: { key: string; label: string; color: string; points: CheckHistory['latency'] },
	format: (v: number) => string,
	yMax?: number
)}
	<div>
		<h3 class="mb-2 text-[13px] font-medium text-fg-muted">{title}</h3>
		<div class="relative">
			<AreaChart
				{label}
				series={[series]}
				from={Date.parse(history!.from)}
				to={Date.parse(history!.to)}
				height={130}
				{format}
				{yMax}
			/>
			{#if empty}
				<p class="absolute inset-0 grid place-items-center pb-6 pl-14 text-[13px] text-fg-subtle">
					{empty}
				</p>
			{/if}
		</div>
	</div>
{/snippet}

{#if history}
	<div class="space-y-5">
		{@render chart(
			check.kind === 'ping' ? 'Round trip' : 'Time to answer',
			`Latency ${over}`,
			{ key: 'latency', label: 'Latency', color: 'var(--color-blue-fg)', points: history.latency },
			(v) => (v === 0 ? '0 ms' : latency(v))
		)}
		{@render chart(
			'Uptime',
			`Uptime ${over}`,
			{ key: 'uptime', label: 'Passed', color: 'var(--color-green-fg)', points: history.uptime },
			(v) => `${v.toFixed(0)}%`,
			100
		)}
	</div>
{:else if failed}
	<p class="grid h-40 place-items-center text-[13px] text-fg-subtle">
		Couldn't load the history. Roost tries again in a minute.
	</p>
{:else}
	<Skeleton class="h-72" />
{/if}
