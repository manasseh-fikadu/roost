<!--
	The containers of one device, by compose project, with a filter above them.
	It shows what `poll` has loaded: a placeholder while the first load is on
	its way, why it failed, or the list, with how stale it is if a refresh
	failed since. Admins get a menu on each container, which asks `onaction` to
	confirm what they pick.
-->
<script lang="ts">
	import ContainerIcon from '@lucide/svelte/icons/container';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import Search from '@lucide/svelte/icons/search';
	import { resolve } from '$app/paths';
	import type { DockerContainerList } from '#lib/api/types.js';
	import { ago, bytes, percent, plural } from '#lib/format.js';
	import { Persisted } from '#lib/files/persisted.svelte.js';
	import Card from '#lib/ui/Card.svelte';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import Switch from '#lib/ui/Switch.svelte';
	import { buttonClass } from '#lib/ui/button.js';
	import ErrorState from '#lib/ui/ErrorState.svelte';
	import type { PendingAction } from './actions.js';
	import ContainerRow, { columns } from './ContainerRow.svelte';
	import { groupRows, totals } from './docker.js';
	import { visibleRows } from './list.js';
	import type { Poll } from './poll.svelte.js';

	interface Props {
		poll: Poll<DockerContainerList>;
		deviceId: string;
		deviceName: string;
		/** Called when an admin picks an action for a container, to confirm it. */
		onaction: (pending: PendingAction) => void;
	}

	let { poll, deviceId, deviceName, onaction }: Props = $props();

	const uid = $props.id();

	let query = $state('');
	// Kept in the browser: stopped containers are noise to some and the point to others.
	const showStopped = new Persisted('bield.docker.showStopped', true);

	// The time moves on every second for "updated 3s ago" and "stopped 3h ago".
	let now = $state(Date.now());
	$effect(() => {
		const timer = setInterval(() => (now = Date.now()), 1000);
		return () => clearInterval(timer);
	});

	let refreshing = $state(false);
	async function refresh() {
		refreshing = true;
		try {
			await poll.reload();
		} finally {
			refreshing = false;
		}
	}

	const all = $derived(poll.data?.containers ?? []);
	const shown = $derived(visibleRows(all, query, showStopped.value));
	const groups = $derived(groupRows(shown));
</script>

{#if poll.error && !poll.data}
	<Card>
		<ErrorState
			title="Couldn't reach Docker on {deviceName}"
			icon={ContainerIcon}
			message={poll.error}
			loading={refreshing}
			onretry={refresh}
		>
			{#snippet actions()}
				<a href={resolve('/devices/[id]', { id: deviceId })} class={buttonClass('ghost')}>
					Open the device
				</a>
			{/snippet}
		</ErrorState>
	</Card>
{:else if !poll.data}
	<Card>
		<div class="space-y-2 p-4">
			{#each { length: 4 }, i (i)}
				<Skeleton class="h-11" />
			{/each}
		</div>
	</Card>
{:else if all.length === 0}
	<Card>
		<EmptyState title="No containers on {deviceName}" icon={ContainerIcon}>
			Start one on {deviceName}, with docker run or Docker Compose, and it shows up here within a
			few seconds. For example:
			<div class="mt-3 text-left"><CodeLine command="docker run -d --name hello nginx" /></div>
		</EmptyState>
	</Card>
{:else}
	<div class="mb-4 flex flex-wrap items-center gap-x-4 gap-y-2">
		<div class="relative w-full sm:w-72">
			<Search
				class="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-fg-faint"
			/>
			<Input
				bind:value={query}
				placeholder="Filter by name, image, port…"
				class="h-8 pl-8 text-[13px]"
				aria-label="Filter containers"
				autocomplete="off"
				spellcheck={false}
			/>
		</div>
		<div class="flex items-center gap-2 text-[13px] text-fg-muted">
			<Switch id="{uid}-stopped" bind:checked={showStopped.value} />
			<label for="{uid}-stopped">Show stopped</label>
		</div>
		<div class="ml-auto flex items-center gap-1.5">
			<span
				class={['num text-[12px]', poll.error ? 'text-amber-fg' : 'text-fg-faint']}
				title={poll.error ?? undefined}
				role="status"
			>
				{poll.error ? 'refresh failed · ' : ''}
				{shown.length !== all.length ? `${shown.length} of ${all.length} · ` : ''}
				updated {ago(poll.at, Math.max(now, poll.at))}
			</span>
			<IconButton label="Refresh containers" size="sm" onclick={refresh}>
				<RefreshCw class={['size-3.5', refreshing && 'animate-spin']} />
			</IconButton>
		</div>
	</div>

	{#if groups.length === 0}
		<p class="py-10 text-center text-[13px] text-fg-subtle">
			{#if query.trim()}
				No containers match “{query.trim()}”.
			{:else}
				No containers are up. Turn on “Show stopped” to see the rest.
			{/if}
		</p>
	{:else}
		<div class="space-y-4">
			{#each groups as g (g.project ?? '')}
				{@const sum = totals(g.containers)}
				<Card
					title={g.project ?? 'Standalone containers'}
					description={g.project ? 'Docker Compose project' : 'Not part of a compose project'}
				>
					{#snippet actions()}
						<span class="flex items-center gap-3 num text-[12px] text-fg-subtle">
							{#if sum.unhealthy > 0}
								<span class="text-red-fg">{sum.unhealthy} unhealthy</span>
							{/if}
							{#if sum.running > 0}
								<span class="hidden sm:inline">{percent(sum.cpu, 1)} · {bytes(sum.mem)}</span>
							{/if}
							<span>{sum.running}/{plural(sum.total, 'container')} running</span>
						</span>
					{/snippet}
					<div
						class={[
							'hidden gap-x-3 border-b border-line px-4 py-2 text-[12px] text-fg-subtle xl:grid',
							columns
						]}
						aria-hidden="true"
					>
						<span>Container</span>
						<span>State</span>
						<span>CPU</span>
						<span>Memory</span>
						<span>Net I/O</span>
						<span>Ports</span>
						<span class="text-right">Restarts</span>
						<span></span>
					</div>
					<ul>
						{#each g.containers as c (c.id)}
							<ContainerRow container={c} {deviceId} {now} {onaction} />
						{/each}
					</ul>
				</Card>
			{/each}
		</div>
	{/if}
{/if}
