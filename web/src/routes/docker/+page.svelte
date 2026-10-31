<script lang="ts">
	import ContainerIcon from '@lucide/svelte/icons/container';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import type { Device } from '#lib/api/types.js';
	import ActionDialog from '#lib/docker/ActionDialog.svelte';
	import type { PendingAction } from '#lib/docker/actions.js';
	import { getDockerContainers, getDockerOverview } from '#lib/docker/api.js';
	import ContainerList from '#lib/docker/ContainerList.svelte';
	import ImagesTab from '#lib/docker/ImagesTab.svelte';
	import { pacedOverview, parseTab } from '#lib/docker/list.js';
	import NetworksTab from '#lib/docker/NetworksTab.svelte';
	import { Poll } from '#lib/docker/poll.svelte.js';
	import Summary from '#lib/docker/Summary.svelte';
	import VolumesTab from '#lib/docker/VolumesTab.svelte';
	import { live } from '#lib/live.svelte.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Select from '#lib/ui/Select.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import TabPanel from '#lib/ui/TabPanel.svelte';
	import Tabs from '#lib/ui/Tabs.svelte';

	/** The machines whose Docker Roost can read. */
	const devices = $derived((live.snapshot?.devices ?? []).filter((d) => d.docker));
	/** The device in ?device=, or else the first. */
	const device = $derived.by((): Device | undefined => {
		const wanted = page.url.searchParams.get('device');
		return devices.find((d) => d.id === wanted) ?? devices[0];
	});
	// The live data replaces every device's object with each snapshot, and anything that read the
	// object would start over: the polls would restart and the filter would be lost. The page uses
	// only what stays equal from one snapshot to the next.
	const deviceId = $derived(device?.id);
	const deviceName = $derived(device?.name);

	// The tab is in the URL, so links can point at it.
	const tab = $derived(parseTab(page.url.searchParams.get('tab')));

	/** Another machine's containers aren't this one's, so the tab and anything else is left behind. */
	function setDevice(id: string) {
		const url = new URL(page.url.href);
		url.search = '';
		url.searchParams.set('device', id);
		void goto(url, { replace: true, reset: false });
	}

	function setTab(t: string) {
		const url = new URL(page.url.href);
		if (t === 'containers') {
			url.searchParams.delete('tab');
		} else {
			url.searchParams.set('tab', t);
		}
		void goto(url, { replace: true, reset: false });
	}

	// The containers feed the tiles as well as the first tab, so they load whichever tab is open.
	// Each device gets polls of its own, which the effects start and the previous device's stop.
	const containers = $derived.by(() => {
		const id = deviceId;
		return id ? new Poll(() => getDockerContainers(id), 5000) : null;
	});
	// The disk takes the daemon a while to measure: ask every few seconds until it has, then rarely.
	const overview = $derived.by(() => {
		const id = deviceId;
		return id
			? new Poll(
					pacedOverview(() => getDockerOverview(id)),
					4000
				)
			: null;
	});
	$effect(() => containers?.start());
	$effect(() => overview?.start());

	/** The container an admin is about to start, stop or restart. */
	let pending = $state<PendingAction | null>(null);

	const tabs = $derived([
		{ value: 'containers', label: 'Containers', count: containers?.data?.containers.length },
		{ value: 'volumes', label: 'Volumes', count: overview?.data?.disk?.volumes.count },
		{ value: 'images', label: 'Images', count: overview?.data?.images },
		{ value: 'networks', label: 'Networks' }
	]);
</script>

<PageHeader
	title="Docker"
	description={deviceName
		? `Containers, volumes, images and networks on ${deviceName}.`
		: 'Containers, volumes, images and networks on your machines.'}
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
		<EmptyState title="No machine has Docker yet" icon={ContainerIcon}>
			Docker on the machine Roost runs on shows up here by itself. For another machine, an admin
			sets up SSH access on its device page, and Roost checks it for Docker.
			<a
				href={resolve('/devices')}
				class="mt-2 block text-fg-muted underline decoration-line-heavy underline-offset-4 hover:text-fg"
				>See the devices</a
			>
		</EmptyState>
	</Card>
{:else}
	<!-- Another device starts over: the filter, the tab's data and any action waiting to be confirmed. -->
	{#key deviceId}
		{#if containers && overview}
			<!-- The tiles say nothing when Docker can't be read; the list says why. -->
			{#if containers.data || !containers.error}
				<div class="mb-6">
					<Summary containers={containers.data?.containers ?? null} overview={overview.data} />
				</div>
			{/if}

			<Tabs bind:value={() => tab, setTab} {tabs}>
				<TabPanel value="containers">
					{#if tab === 'containers'}
						<ContainerList
							poll={containers}
							deviceId={deviceId!}
							deviceName={deviceName!}
							onaction={(p) => (pending = p)}
						/>
					{/if}
				</TabPanel>
				<TabPanel value="volumes">
					{#if tab === 'volumes'}
						<VolumesTab deviceId={deviceId!} />
					{/if}
				</TabPanel>
				<TabPanel value="images">
					{#if tab === 'images'}
						<ImagesTab deviceId={deviceId!} />
					{/if}
				</TabPanel>
				<TabPanel value="networks">
					{#if tab === 'networks'}
						<NetworksTab deviceId={deviceId!} />
					{/if}
				</TabPanel>
			</Tabs>

			<AdminOnly>
				<ActionDialog
					deviceId={deviceId!}
					deviceName={deviceName!}
					bind:pending
					ondone={() => containers.reload()}
				/>
			</AdminOnly>
		{/if}
	{/key}
{/if}
