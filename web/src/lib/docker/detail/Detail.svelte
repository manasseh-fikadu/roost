<!--
	One container's page, once the device is known: the container polled every
	10 seconds, its live stats while it runs, and a tab for each thing worth
	reading about it. The tab is in the URL (?tab=), so links can point at it.
	The parent keys it by device and container name, so it starts over when
	either changes.
-->
<script lang="ts">
	import Box from '@lucide/svelte/icons/box';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { isAdmin } from '#lib/access.js';
	import { ApiError } from '#lib/api/client.js';
	import type { DockerContainerDetail } from '#lib/api/types.js';
	import { buttonClass } from '#lib/ui/button.js';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import TabPanel from '#lib/ui/TabPanel.svelte';
	import Tabs from '#lib/ui/Tabs.svelte';
	import ActionDialog from '../ActionDialog.svelte';
	import type { PendingAction } from '../actions.js';
	import { getDockerContainer, type DockerAction } from '../api.js';
	import HistoryCharts from '../HistoryCharts.svelte';
	import LiveCharts from '../LiveCharts.svelte';
	import { LiveStats } from '../livestats.svelte.js';
	import LogViewer from '../LogViewer.svelte';
	import { Poll } from '../poll.svelte.js';
	import AdminNotice from './AdminNotice.svelte';
	import EnvPanel from './EnvPanel.svelte';
	import Header from './Header.svelte';
	import { parseTab, statsEnded } from './helpers.js';
	import InspectPanel from './InspectPanel.svelte';
	import LabelsPanel from './LabelsPanel.svelte';
	import MountsPanel from './MountsPanel.svelte';
	import NetworkPanel from './NetworkPanel.svelte';
	import Overview from './Overview.svelte';
	import ProcessesPanel from './ProcessesPanel.svelte';

	interface Props {
		deviceId: string;
		deviceName: string;
		/** The container's name, or ID, as in the URL. */
		name: string;
		/** Where the list of this device's containers is. */
		backHref: string;
		/** Whether Roost can open the machine's files, which admins can follow a path into. */
		filesReady: boolean;
	}

	let { deviceId, deviceName, name, backHref, filesReady }: Props = $props();

	const admin = $derived(isAdmin(page.data.me));
	const canBrowse = $derived(admin && filesReady);
	const tab = $derived(parseTab(page.url.searchParams.get('tab')));

	function setTab(t: string) {
		const url = new URL(page.url.href);
		if (t === 'overview') {
			url.searchParams.delete('tab');
		} else {
			url.searchParams.set('tab', t);
		}
		void goto(url, { replace: true, reset: false });
	}

	/** Whether the container is gone, as the last load said. */
	let missing = $state(false);
	const detail = new Poll<DockerContainerDetail>(async () => {
		try {
			const value = await getDockerContainer(deviceId, name);
			missing = false;
			return value;
		} catch (err) {
			missing = err instanceof ApiError && err.status === 404;
			throw err;
		}
	}, 10_000);

	$effect(() => detail.start());

	// Ages tick on their own, between the polls.
	let now = $state(Date.now());
	$effect(() => {
		const timer = setInterval(() => (now = Date.now()), 5000);
		return () => clearInterval(timer);
	});

	const d = $derived(detail.data);
	// The effects below read these, not `d`, which is a new object with every poll.
	const containerId = $derived(d?.id ?? null);
	const containerName = $derived(d?.name ?? name);
	const startedAt = $derived(d?.state.startedAt);
	const running = $derived(d?.state.running ?? false);
	const streaming = $derived(running && !d?.state.paused);
	const memLimit = $derived(d?.resources.memory ?? null);

	const stats = new LiveStats();
	$effect(() => {
		// A container that restarted is a new run, and its stream a new one.
		void startedAt;
		return stats.connect(deviceId, containerId, streaming);
	});

	let pending = $state<PendingAction | null>(null);

	function ask(action: DockerAction) {
		if (d) {
			pending = { id: d.id, name: d.name, state: d.state.status, action };
		}
	}

	const tabs = $derived([
		{ value: 'overview', label: 'Overview' },
		{ value: 'logs', label: 'Logs' },
		{ value: 'stats', label: 'Stats' },
		{ value: 'processes', label: 'Processes' },
		{ value: 'mounts', label: 'Mounts', count: d?.mounts.length },
		{ value: 'network', label: 'Network' },
		{ value: 'env', label: 'Environment', count: admin ? (d?.env?.length ?? 0) : undefined },
		{
			value: 'labels',
			label: 'Labels',
			count: d ? Object.keys(d.labels).length + Object.keys(d.secretLabels ?? {}).length : undefined
		},
		{ value: 'inspect', label: 'Inspect' }
	]);
</script>

{#if d}
	<Header container={d} {deviceName} {admin} onaction={ask} />

	{#if detail.error}
		<p
			role="alert"
			class="mb-4 rounded-lg border border-amber/25 bg-amber/5 px-4 py-2.5 text-[13px] break-words text-amber-fg"
		>
			Refresh failed: {detail.error}
		</p>
	{/if}

	<Tabs bind:value={() => tab, setTab} {tabs}>
		<TabPanel value="overview">
			{#if tab === 'overview'}
				<Overview container={d} samples={stats.samples} {now} {admin} {deviceId} {canBrowse} />
			{/if}
		</TabPanel>
		<TabPanel value="logs">
			{#if tab === 'logs'}
				<AdminOnly>
					<LogViewer {deviceId} container={containerName} {running} tty={d.tty} />
					{#snippet fallback()}<AdminNotice what="read container logs" />{/snippet}
				</AdminOnly>
			{/if}
		</TabPanel>
		<TabPanel value="stats">
			{#if tab === 'stats'}
				<div class="space-y-4">
					<LiveCharts samples={stats.samples} ended={statsEnded(d.state, stats.ended)} {memLimit} />
					<HistoryCharts {deviceId} container={containerName} {memLimit} />
				</div>
			{/if}
		</TabPanel>
		<TabPanel value="processes">
			{#if tab === 'processes'}
				<AdminOnly>
					<ProcessesPanel {deviceId} container={containerName} running={streaming} />
					{#snippet fallback()}<AdminNotice what="see container processes" />{/snippet}
				</AdminOnly>
			{/if}
		</TabPanel>
		<TabPanel value="mounts">
			{#if tab === 'mounts'}
				<MountsPanel mounts={d.mounts} {deviceId} {canBrowse} />
			{/if}
		</TabPanel>
		<TabPanel value="network">
			{#if tab === 'network'}
				<NetworkPanel container={d} />
			{/if}
		</TabPanel>
		<TabPanel value="env">
			{#if tab === 'env'}
				<AdminOnly>
					<EnvPanel container={d} />
					{#snippet fallback()}<AdminNotice what="read environment variables" />{/snippet}
				</AdminOnly>
			{/if}
		</TabPanel>
		<TabPanel value="labels">
			{#if tab === 'labels'}
				<LabelsPanel container={d} />
			{/if}
		</TabPanel>
		<TabPanel value="inspect">
			{#if tab === 'inspect'}
				<AdminOnly>
					<InspectPanel {deviceId} container={containerName} />
					{#snippet fallback()}<AdminNotice what="read the inspection" />{/snippet}
				</AdminOnly>
			{/if}
		</TabPanel>
	</Tabs>

	{#if admin}
		<ActionDialog {deviceId} {deviceName} bind:pending ondone={() => void detail.reload()} />
	{/if}
{:else if detail.error}
	<Card>
		{#if missing}
			<EmptyState title="No container named {name}" icon={Box}>
				It may have been removed, or renamed.
				<div class="mt-3"><a href={backHref} class={buttonClass()}>All containers</a></div>
			</EmptyState>
		{:else}
			<EmptyState title="Couldn't load this container" icon={Box}>
				<span class="font-mono text-[12px] break-words text-fg-subtle">{detail.error}</span>
				<div class="mt-3 flex justify-center gap-2">
					<Button onclick={() => void detail.reload()}>Retry</Button>
					<a href={backHref} class={buttonClass('ghost')}>All containers</a>
				</div>
			</EmptyState>
		{/if}
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
