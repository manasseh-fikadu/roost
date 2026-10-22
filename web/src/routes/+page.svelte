<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import Bell from '@lucide/svelte/icons/bell';
	import { resolve } from '$app/paths';
	import type { CollectorState } from '#lib/api/types.js';
	import HealthRing from '#lib/charts/HealthRing.svelte';
	import DevicesTable from '#lib/devices/DevicesTable.svelte';
	import { plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import MapLegend from '#lib/map/MapLegend.svelte';
	import NetworkMap from '#lib/map/NetworkMap.svelte';
	import type { Status } from '#lib/status.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import Tooltip from '#lib/ui/Tooltip.svelte';

	const devices = $derived(live.snapshot?.devices ?? []);
	const online = $derived(devices.filter((d) => d.online).length);
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());

	const collectorStatus: Record<CollectorState, Status> = {
		ok: 'online',
		failing: 'attention',
		waiting: 'unknown'
	};
	const collectorText: Record<CollectorState, string> = {
		ok: 'Working',
		failing: 'Failing',
		waiting: 'Waiting for its first run'
	};
</script>

<PageHeader
	title="Overview"
	description={live.snapshot
		? `${online} of ${plural(devices.length, 'machine')} online`
		: 'Connecting to the hub…'}
/>

<div class="mb-6 flex min-h-7 flex-wrap gap-2">
	{#each live.snapshot?.collectors ?? [] as c (c.name)}
		<Tooltip content={c.error ? `${collectorText[c.state]}: ${c.error}` : collectorText[c.state]}>
			{#snippet trigger({ props })}
				<span
					{...props}
					class={[
						'inline-flex h-7 items-center gap-2 rounded-full border bg-surface px-3 font-mono text-[12px]',
						c.state === 'failing'
							? 'border-amber/30 text-amber-fg'
							: 'border-line-strong text-fg-muted'
					]}
				>
					<StatusDot status={collectorStatus[c.state]} />
					{c.name}
					{#if c.state === 'failing'}
						<span>· failing</span>
					{/if}
					<span class="sr-only">: {collectorText[c.state]}{c.error ? `, ${c.error}` : ''}</span>
				</span>
			{/snippet}
		</Tooltip>
	{:else}
		<Skeleton class="h-7 w-36 rounded-full" />
		<Skeleton class="h-7 w-40 rounded-full" />
	{/each}
</div>

<div class="grid gap-4 xl:grid-cols-[minmax(0,1fr)_340px]">
	<Card
		title="Network map"
		description="How the hub reaches the internet and every machine on the tailnet"
		bodyClass="p-2"
	>
		<div class="px-2 pt-1 pb-2"><MapLegend /></div>
		{#if live.snapshot}
			<NetworkMap snapshot={live.snapshot} />
		{:else}
			<Skeleton class="h-130 rounded-lg" />
		{/if}
	</Card>
	<div class="space-y-4">
		<Card title="Network health">
			<div class="flex items-center gap-4 p-5">
				<HealthRing score={null} />
				<div class="min-w-0">
					<p class="text-[15px] font-medium text-fg-muted">No checks yet</p>
					<p class="mt-0.5 text-[13px] text-fg-subtle">
						The score comes from your checks, with #25.
					</p>
				</div>
			</div>
		</Card>
		<Card title="Alerts">
			<EmptyState title="No alerts" icon={Bell}>Roost starts raising alerts with #26.</EmptyState>
		</Card>
	</div>
</div>

<Card title="Machines" class="mt-4">
	{#snippet actions()}
		<a
			href={resolve('/devices')}
			class="flex items-center gap-1 text-[12px] text-fg-subtle transition-colors hover:text-fg"
		>
			All devices <ArrowRight class="size-3" />
		</a>
	{/snippet}
	{#if live.snapshot}
		<DevicesTable {devices} {now} />
	{:else}
		<Skeleton class="m-4 h-24" />
	{/if}
</Card>
