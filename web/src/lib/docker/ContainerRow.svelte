<!--
	A container in the list: its state, what it uses, its ports and how long it
	has been up or down. Admins get a menu to start, stop or restart it, which
	asks `onaction` to confirm. On a wide screen it's a row of columns, which
	the header over the rows (see `columns`) names; on a narrower one, the name
	and then a grid of what the columns would hold.
-->
<script lang="ts" module>
	/** The columns of a row on a wide screen. The header over the rows has the same. */
	export const columns =
		'xl:grid-cols-[minmax(0,1.7fr)_minmax(0,1.3fr)_88px_minmax(0,1.1fr)_96px_minmax(0,1fr)_44px_32px]';
</script>

<script lang="ts">
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import type { DockerContainerRow } from '#lib/api/types.js';
	import { bytes, percent, plural, rateShort } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import Badge from '#lib/ui/Badge.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import Meter from '#lib/ui/Meter.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import { actionCopy, actionIcons, type PendingAction } from './actions.js';
	import { containerHref } from './containerLink.js';
	import { actionsFor, rowStatus } from './docker.js';
	import { cpuShare, memShare, portChips, uptimeLabel } from './list.js';
	import StateBadge from './StateBadge.svelte';

	interface Props {
		container: DockerContainerRow;
		deviceId: string;
		/** When "stopped 3h ago" is measured from, in Unix milliseconds. */
		now: number;
		/** Called when an admin picks an action, to confirm it. */
		onaction: (pending: PendingAction) => void;
	}

	let { container: c, deviceId, now, onaction }: Props = $props();

	const status = $derived(rowStatus(c));
	const running = $derived(c.state === 'running');
	const s = $derived(c.stats);
	const ports = $derived(portChips(c.ports));
	const memTitle = $derived(
		c.memLimitSet
			? undefined
			: "No memory limit of its own: the bar is its share of the machine's memory"
	);
</script>

{#snippet label(text: string)}
	<span class="mb-0.5 block font-mono text-[10px] tracking-wide text-fg-faint uppercase xl:sr-only">
		{text}
	</span>
{/snippet}

<li
	class={[
		'grid grid-cols-[minmax(0,1fr)_auto] items-start gap-x-3 gap-y-2.5 border-b border-line px-4 py-3 transition-colors last:border-0 hover:bg-hover/40 xl:items-center xl:py-2.5',
		columns
	]}
>
	<div class="flex min-w-0 items-center gap-2.5">
		<StatusDot {status} pulse={status === 'online'} />
		<div class="min-w-0">
			<a
				href={containerHref(deviceId, c.name)}
				class={[
					'block truncate text-[14px] font-medium hover:underline xl:text-[13.5px]',
					running ? 'text-fg' : 'text-fg-muted'
				]}
			>
				{c.name}
			</a>
			<span class="block truncate font-mono text-[11px] text-fg-subtle" title={c.image}>
				{c.image}
			</span>
		</div>
	</div>

	<AdminOnly>
		<div class="-my-1 xl:order-last xl:my-0">
			<Menu>
				{#snippet trigger({ props })}
					<IconButton {...props} label="Actions for {c.name}">
						<Ellipsis class="size-4" />
					</IconButton>
				{/snippet}
				{#each actionsFor(c.state) as action (action)}
					{@const Icon = actionIcons[action]}
					<MenuItem
						icon={Icon}
						disabled={live.demo}
						onSelect={() => onaction({ id: c.id, name: c.name, state: c.state, action })}
					>
						{actionCopy[action].label}
					</MenuItem>
				{/each}
			</Menu>
		</div>
	</AdminOnly>

	<div class="col-span-2 grid grid-cols-2 gap-x-3 gap-y-2.5 sm:grid-cols-3 xl:contents">
		<div class="min-w-0">
			{@render label('State')}
			<StateBadge container={c} />
			<div class="mt-0.5 truncate num text-[11px] text-fg-subtle" title={c.error || c.status}>
				{#if c.state === 'running' && c.health === 'healthy'}
					<span class="text-green-fg">healthy</span> ·
				{/if}
				{uptimeLabel(c, now)}
			</div>
		</div>

		<div class="min-w-0">
			{@render label('CPU')}
			<div class="num text-[12px] text-fg">{s ? percent(s.cpu, 1) : '—'}</div>
			{#if s}
				<Meter value={cpuShare(s)} warn={75} crit={90} class="mt-1" />
			{/if}
		</div>

		<div class="min-w-0" title={s ? memTitle : undefined}>
			{@render label('Memory')}
			<div class="truncate num text-[12px] text-fg">
				{#if s?.memUsed != null}
					{bytes(s.memUsed)}
					<span class="text-fg-faint">
						{c.memLimitSet && s.memLimit ? `/ ${bytes(s.memLimit, 0)}` : '· no limit'}
					</span>
				{:else}
					—
				{/if}
			</div>
			{#if s}
				<Meter value={memShare(s)} class="mt-1" />
			{/if}
		</div>

		<div class="min-w-0">
			{@render label('Network')}
			<div class="truncate num text-[11.5px] text-fg-muted">
				{s ? `↓${rateShort(s.netRx)} ↑${rateShort(s.netTx)}` : '—'}
			</div>
		</div>

		<div class="min-w-0">
			{@render label('Ports')}
			{#if ports.shown.length > 0}
				<div class="flex min-w-0 flex-wrap gap-1">
					{#each ports.shown as p (p.label)}
						<span title={p.title} class="min-w-0">
							<Badge mono class={p.published ? undefined : 'opacity-60'}>{p.label}</Badge>
						</span>
					{/each}
					{#if ports.more > 0}
						<Badge mono>+{ports.more}</Badge>
					{/if}
				</div>
			{:else}
				<span class="text-fg-faint">—</span>
			{/if}
		</div>

		<div class={['min-w-0 xl:text-right', c.restartCount === 0 && 'max-xl:hidden']}>
			{@render label('Restarts')}
			{#if c.restartCount > 0}
				<span
					class="num text-[12px] text-amber-fg"
					title="Docker restarted it {plural(c.restartCount, 'time')}"
				>
					{c.restartCount}
				</span>
			{/if}
		</div>
	</div>
</li>
