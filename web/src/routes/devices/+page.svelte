<script lang="ts">
	import Bot from '@lucide/svelte/icons/bot';
	import Monitor from '@lucide/svelte/icons/monitor';
	import { resolve } from '$app/paths';
	import type { Status } from '#lib/api/types.js';
	import DevicesTable from '#lib/devices/DevicesTable.svelte';
	import { plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { statusMeta } from '#lib/status.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import { buttonClass } from '#lib/ui/button.js';

	type Filter = 'all' | Status;

	let filter: Filter = $state('all');

	const devices = $derived(live.snapshot?.devices ?? []);
	const shown = $derived(filter === 'all' ? devices : devices.filter((d) => d.status === filter));
	const now = $derived(live.snapshot ? Date.parse(live.snapshot.generatedAt) : Date.now());

	function count(s: Status) {
		return devices.filter((d) => d.status === s).length;
	}

	const options = $derived([
		{ value: 'all' as Filter, label: `All ${devices.length}` },
		...(['online', 'attention', 'offline'] as const).map((s) => ({
			value: s as Filter,
			label: `${statusMeta[s].label}: ${count(s)}`
		}))
	]);
</script>

<PageHeader
	title="Devices"
	description={live.snapshot
		? `${plural(devices.length, 'machine')} on the tailnet, and the path from the hub to each.`
		: 'Connecting to the hub…'}
>
	{#snippet actions()}
		<a href={resolve('/devices/agents')} class={buttonClass()}>
			<Bot class="size-3.5" /> Agents
		</a>
		{#if live.snapshot}
			<Segmented bind:value={filter} {options} label="Show devices">
				{#snippet option(o)}
					{#if o.value === 'all'}
						{o.label}
					{:else}
						<span class="flex items-center gap-1.5">
							<StatusDot status={o.value} />
							<span class="num">{count(o.value)}</span>
						</span>
					{/if}
				{/snippet}
			</Segmented>
		{/if}
	{/snippet}
</PageHeader>

<Card>
	{#if !live.snapshot}
		{#each [1, 2, 3] as i (i)}
			<div class="flex items-center gap-3 border-b border-line px-4 py-3 last:border-0">
				<Skeleton class="size-8" />
				<div class="flex-1 space-y-1.5">
					<Skeleton class="h-4 w-40" />
					<Skeleton class="h-3 w-56" />
				</div>
			</div>
		{/each}
	{:else if shown.length}
		<DevicesTable devices={shown} {now} detailed />
	{:else}
		<EmptyState title="No devices here" icon={Monitor}>
			{filter === 'all' ? 'Nothing is on the tailnet yet.' : 'No device has this status now.'}
		</EmptyState>
	{/if}
</Card>
<p class="mt-4 text-[13px] text-fg-subtle">
	Roost measures the machine it runs on. Install the
	<a
		href={resolve('/devices/agents')}
		class="text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg">agent</a
	> on the others to see their CPU, memory and disk.
</p>
