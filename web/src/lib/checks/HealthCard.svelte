<!--
	The health score on Overview: the share of live checks that pass, how
	many pass, fail or can't run, and the score over the last day, which
	loads again every minute.
-->
<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { resolve } from '$app/paths';
	import { getHealthHistory } from '#lib/api/client.js';
	import type { HealthHistory, HealthScore } from '#lib/api/types.js';
	import HealthRing from '#lib/charts/HealthRing.svelte';
	import Sparkline from '#lib/charts/Sparkline.svelte';
	import { healthLabels, scoreClass, scoreColor, type CheckFilter } from '#lib/checks.js';
	import { plural } from '#lib/format.js';
	import { stepValues } from '#lib/history.js';
	import Card from '#lib/ui/Card.svelte';

	interface Props {
		/** The health score, or null before the first snapshot. */
		health: HealthScore | null;
	}

	let { health }: Props = $props();

	let history = $state.raw<HealthHistory | null>(null);

	$effect(() => {
		let current = true;
		async function load() {
			try {
				const h = await getHealthHistory('24h');
				if (current) {
					history = h;
				}
			} catch {
				// The card keeps the history it has, and tries again.
			}
		}
		load();
		const timer = setInterval(load, 60_000);
		return () => {
			current = false;
			clearInterval(timer);
		};
	});

	const score = $derived(health?.score ?? null);
	const live = $derived(health ? health.passing + health.failing : 0);
	const values = $derived(history ? stepValues(history, history.score) : []);
	const sampled = $derived(values.some((v) => v !== null));
	const counts: {
		filter: CheckFilter;
		label: string;
		key: 'passing' | 'failing' | 'unavailable';
	}[] = [
		{ filter: 'passing', label: 'passing', key: 'passing' },
		{ filter: 'failing', label: 'failing', key: 'failing' },
		{ filter: 'unavailable', label: 'unavailable', key: 'unavailable' }
	];
</script>

<Card title="Network health">
	{#snippet actions()}
		<a
			href={resolve('/checks')}
			class="flex items-center gap-1 text-[12px] text-fg-subtle transition-colors hover:text-fg"
		>
			Checks <ArrowRight class="size-3" />
		</a>
	{/snippet}
	<div class="p-4">
		<div class="flex items-center gap-4">
			<HealthRing {score} />
			<div class="min-w-0">
				<p class={['text-[15px] font-medium', scoreClass(score)]}>
					{health ? healthLabels[health.label] : 'Connecting…'}
				</p>
				<p class="mt-0.5 text-[13px] text-fg-subtle">
					{#if !health}
						Waiting for the hub
					{:else if health.total === 0}
						Add a check to get a score
					{:else if live === 0}
						No check has a result yet
					{:else}
						{health.passing} of {plural(live, 'live check')} passing
					{/if}
				</p>
			</div>
		</div>
		<div class="mt-4 grid grid-cols-3 divide-x divide-line rounded-lg border border-line bg-canvas">
			{#each counts as c (c.key)}
				{@const n = health?.[c.key] ?? 0}
				<a
					href="{resolve('/checks')}?state={c.filter}"
					class="px-2 py-2.5 text-center transition-colors hover:bg-hover/50"
				>
					<span
						class={[
							'block num text-[16px] font-medium',
							c.key === 'failing' && n > 0
								? 'text-red-fg'
								: c.key === 'unavailable'
									? 'text-fg-muted'
									: 'text-fg'
						]}
					>
						{health ? n : '—'}
					</span>
					<span class="block text-[11.5px] text-fg-subtle">{c.label}</span>
				</a>
			{/each}
		</div>
		<div class="mt-4">
			{#if sampled}
				<Sparkline {values} height={36} color={scoreColor(score)} min={0} max={100} />
			{:else}
				<div class="grid h-9 place-items-center text-[12px] text-fg-faint">
					The score's history starts with its first minute.
				</div>
			{/if}
			<div class="mt-1.5 flex justify-between font-mono text-[10.5px] text-fg-faint">
				<span>score, sampled every minute</span>
				<span>24h</span>
			</div>
		</div>
	</div>
</Card>
