<script lang="ts">
	import Bell from '@lucide/svelte/icons/bell';
	import Network from '@lucide/svelte/icons/network';
	import HealthRing from '#lib/charts/HealthRing.svelte';
	import { plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import type { Status } from '#lib/status.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import Tooltip from '#lib/ui/Tooltip.svelte';
	import type { CollectorState } from '#lib/api/types.js';

	const devices = $derived(live.snapshot?.devices ?? []);
	const online = $derived(devices.filter((d) => d.online).length);

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
		description="Live endpoint telemetry across the tailnet and this hub"
		bodyClass="map-grid min-h-80 grid place-items-center"
	>
		<EmptyState title="The map comes next" icon={Network}>
			The internet and DERP, then this hub, then every peer and the path to it. Coming with #23.
		</EmptyState>
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
