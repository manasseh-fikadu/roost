<!--
	A container's page: /docker/{name}?device={id}&tab={tab}. This works out
	which device it is on and shows the container once the device is known.
-->
<script lang="ts">
	import ArrowLeft from '@lucide/svelte/icons/arrow-left';
	import Container from '@lucide/svelte/icons/container';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import Detail from '#lib/docker/detail/Detail.svelte';
	import { live } from '#lib/live.svelte.js';
	import { listedDeviceId, terminalDevices } from '#lib/terminal.js';
	import { buttonClass } from '#lib/ui/button.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	// The live data replaces every device's object with each snapshot, and anything that read the
	// object would start over. The page keeps to a name and a flag, which stay equal from one
	// snapshot to the next.
	const deviceId = $derived(page.url.searchParams.get('device') ?? '');
	const deviceName = $derived(live.snapshot?.devices.find((d) => d.id === deviceId)?.name);
	const name = $derived(page.params.name ?? '');
	/** Whether Files can open this machine, which a path in a mount or a compose project needs. */
	const filesReady = $derived.by(() => {
		const all = live.snapshot?.devices ?? [];
		const listed = listedDeviceId(all, deviceId);
		return terminalDevices(all).some((d) => d.id === listed);
	});

	const listHref = $derived(
		deviceId ? `${resolve('/docker')}?device=${encodeURIComponent(deviceId)}` : resolve('/docker')
	);
</script>

<a
	href={listHref}
	class="mb-4 inline-flex items-center gap-1.5 text-[13px] text-fg-subtle transition-colors hover:text-fg"
>
	<ArrowLeft class="size-3.5" /> Docker{deviceName ? ` · ${deviceName}` : ''}
</a>

{#if deviceName !== undefined}
	{#key `${deviceId}/${name}`}
		<Detail {deviceId} {deviceName} {name} backHref={listHref} {filesReady} />
	{/key}
{:else if live.snapshot}
	<Card>
		<EmptyState title={deviceId ? 'No device here' : 'Which machine?'} icon={Container}>
			{#if deviceId}
				It may have left the tailnet.
			{:else}
				This link doesn't say which machine {name} runs on.
			{/if}
			<div class="mt-3"><a href={listHref} class={buttonClass()}>All containers</a></div>
		</EmptyState>
	</Card>
{:else}
	<div class="space-y-4">
		<Skeleton class="h-12 w-80 max-w-full" />
		<Skeleton class="h-10" />
		<div class="grid grid-cols-2 gap-3 md:grid-cols-4 xl:grid-cols-7">
			{#each { length: 7 } as _, i (i)}
				<Skeleton class="h-24 rounded-xl" />
			{/each}
		</div>
	</div>
{/if}
