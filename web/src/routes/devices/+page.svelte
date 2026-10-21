<script lang="ts">
	import { resolve } from '$app/paths';
	import { deviceIcon, deviceStatus, shortName } from '#lib/device.js';
	import { plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Card from '#lib/ui/Card.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusPill from '#lib/ui/StatusPill.svelte';

	const devices = $derived(live.snapshot?.devices ?? []);
</script>

<PageHeader
	title="Devices"
	description={live.snapshot
		? `${plural(devices.length, 'machine')} on the tailnet`
		: 'Connecting to the hub…'}
/>

<Card bodyClass="divide-y divide-line">
	{#each devices as d (d.id)}
		{@const Icon = deviceIcon(d)}
		<a
			href={resolve('/devices/[id]', { id: d.id })}
			class="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-hover/50"
		>
			<span
				class="flex size-8 shrink-0 items-center justify-center rounded-md border border-line-strong bg-raised text-fg-subtle"
			>
				<Icon class="size-4" strokeWidth={1.6} />
			</span>
			<span class="min-w-0 flex-1">
				<span class="flex items-center gap-2">
					<span class="truncate text-[14px] font-medium text-fg">{d.name}</span>
					{#if d.self}
						<Badge>This hub</Badge>
					{/if}
				</span>
				<span class="block truncate font-mono text-[11.5px] text-fg-faint">
					{shortName(d)} · {d.ips[0] ?? 'no address'}
				</span>
			</span>
			<StatusPill status={deviceStatus(d)} />
		</a>
	{:else}
		{#each [1, 2, 3] as i (i)}
			<div class="flex items-center gap-3 px-4 py-3">
				<Skeleton class="size-8" />
				<div class="flex-1 space-y-1.5">
					<Skeleton class="h-4 w-40" />
					<Skeleton class="h-3 w-56" />
				</div>
			</div>
		{/each}
	{/each}
</Card>
<p class="mt-4 text-[13px] text-fg-subtle">
	Paths, latency, metrics, agents and settings for each device come with #23 and #24.
</p>
