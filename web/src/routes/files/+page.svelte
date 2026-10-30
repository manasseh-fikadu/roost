<script lang="ts">
	import FolderOpen from '@lucide/svelte/icons/folder-open';
	import ShieldAlert from '@lucide/svelte/icons/shield-alert';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import type { Device } from '#lib/api/types.js';
	import FileBrowser from '#lib/files/FileBrowser.svelte';
	import { live } from '#lib/live.svelte.js';
	import { canOpen, listedDeviceId, terminalDevices } from '#lib/terminal.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	const all = $derived(live.snapshot?.devices ?? []);
	/** The machines Roost can log in to, which is also where it can open their files. */
	const devices = $derived(terminalDevices(all));
	/**
	 * The device in ?device=, or else the machine Roost runs on, or else the
	 * first. The machine Roost runs on can be two nodes that share one access,
	 * and the list has only one of them.
	 */
	const device = $derived.by((): Device | undefined => {
		const wanted = page.url.searchParams.get('device');
		const id = wanted && listedDeviceId(all, wanted);
		return devices.find((d) => d.id === id) ?? devices.find((d) => d.ssh?.hub) ?? devices[0];
	});
	const ready = $derived(!!device && canOpen(device));
	// The live data replaces every device's object with each snapshot, and anything that read the
	// object would start over: a folder would reload and an open editor would lose its text. The
	// browser gets only what it needs, which stays equal from one snapshot to the next.
	const deviceId = $derived(device?.id);
	const deviceOs = $derived(device?.os);

	function setDevice(id: string) {
		// Another machine's folders aren't this one's, so the path and the file stay behind.
		const url = new URL(page.url.href);
		url.search = '';
		url.searchParams.set('device', id);
		void goto(url, { replace: true, reset: false });
	}
</script>

<AdminOnly>
	<PageHeader
		title="Files"
		description="Browse, preview, edit, upload and download files on any machine Roost has SSH access to."
	>
		{#snippet actions()}
			{#if device && devices.length > 1}
				<Select
					value={device.id}
					onchange={(e) => setDevice(e.currentTarget.value)}
					class="h-8 w-52 text-[13px]"
					aria-label="Device"
				>
					{#each devices as d (d.id)}
						<option value={d.id}>{d.name}{d.online || d.ssh?.hub ? '' : ' (offline)'}</option>
					{/each}
				</Select>
			{/if}
		{/snippet}
	</PageHeader>

	{#if !live.snapshot}
		<Skeleton class="h-96 rounded-xl" />
	{:else if !device}
		<Card>
			<EmptyState title="No machine has SSH access yet" icon={FolderOpen}>
				Open a device, and set up Roost's access in its SSH access card. On the machine Roost runs
				on, it takes one click.
				<a
					href={resolve('/devices')}
					class="mt-2 block text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg"
					>See the devices</a
				>
			</EmptyState>
		</Card>
	{:else if !ready}
		<Card>
			<EmptyState
				title={device.ssh?.state === 'host-key-changed'
					? `${device.name}'s host key changed`
					: `${device.name} is offline`}
				icon={ShieldAlert}
			>
				{#if device.ssh?.state === 'host-key-changed'}
					Roost won't log in until an admin trusts the new key, on <a
						href="{resolve('/devices/[id]', { id: device.id })}#ssh"
						class="text-fg underline underline-offset-4">its page</a
					>.
				{:else}
					Files open again once it's back on the tailnet.
				{/if}
			</EmptyState>
		</Card>
	{:else}
		{#key deviceId}
			<FileBrowser deviceId={deviceId!} os={deviceOs!} />
		{/key}
	{/if}
</AdminOnly>
