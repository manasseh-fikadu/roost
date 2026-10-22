<script lang="ts">
	import Monitor from '@lucide/svelte/icons/monitor';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { deviceStatus } from '#lib/device.js';
	import { live } from '#lib/live.svelte.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusPill from '#lib/ui/StatusPill.svelte';

	const device = $derived(live.snapshot?.devices.find((d) => d.id === page.params.id));
</script>

{#if device}
	<PageHeader title={device.name} description={device.dnsName}>
		{#snippet actions()}
			<StatusPill status={deviceStatus(device)} />
		{/snippet}
	</PageHeader>
	<div class="grid gap-4 lg:grid-cols-2">
		<Card title="Tailscale">
			<dl class="grid grid-cols-[auto_1fr] gap-x-6 gap-y-2 p-4 text-[13px]">
				<dt class="font-mono text-[11px] tracking-wide text-fg-faint uppercase">OS</dt>
				<dd class="text-fg">{device.os || '—'}</dd>
				<dt class="font-mono text-[11px] tracking-wide text-fg-faint uppercase">Tailnet IP</dt>
				<dd class="num text-fg">{device.ips.join(', ') || '—'}</dd>
				<dt class="font-mono text-[11px] tracking-wide text-fg-faint uppercase">ID</dt>
				<dd class="truncate num text-fg-muted">{device.id}</dd>
			</dl>
		</Card>
		<Card title="System">
			<EmptyState title="Coming soon" icon={Monitor}>
				The path to this device, its latency, metrics and settings come with #23.
			</EmptyState>
		</Card>
	</div>
{:else if live.snapshot}
	<EmptyState title="No device here" icon={Monitor}>
		It may have left the tailnet. <a
			href={resolve('/devices')}
			class="text-fg underline underline-offset-4">See every device</a
		>.
	</EmptyState>
{:else}
	<Skeleton class="h-8 w-48" />
	<Skeleton class="mt-2 h-4 w-72" />
	<div class="mt-6 grid gap-4 lg:grid-cols-2">
		<Skeleton class="h-40 rounded-xl" />
		<Skeleton class="h-40 rounded-xl" />
	</div>
{/if}
