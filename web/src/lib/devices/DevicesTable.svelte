<!--
	A table of devices: each one's status, the path to it, its metrics and when
	it was last seen, with a link to its page. Phones get a list instead.
-->
<script lang="ts">
	import { resolve } from '$app/paths';
	import type { Device } from '#lib/api/types.js';
	import { deviceIcon, shortName } from '#lib/device.js';
	import { ago, osLabel, percent, ratio } from '#lib/format.js';
	import { statusMeta } from '#lib/status.js';
	import { pathLabel, slowLink, tailnetIP } from '#lib/tailnet.js';
	import Meter from '#lib/ui/Meter.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	interface Props {
		devices: Device[];
		/** When "ago" is measured from, in Unix milliseconds: the snapshot's time. */
		now: number;
		/** Adds each device's tailnet IP. */
		detailed?: boolean;
	}

	let { devices, now, detailed = false }: Props = $props();

	function href(d: Device) {
		return resolve('/devices/[id]', { id: d.id });
	}
</script>

{#snippet icon(d: Device)}
	{@const Icon = deviceIcon(d)}
	<span
		class="flex size-8 shrink-0 items-center justify-center rounded-md border border-line bg-raised text-fg-muted"
	>
		<Icon class="size-4" strokeWidth={1.6} />
	</span>
{/snippet}

{#snippet status(d: Device)}
	<span class="flex items-center gap-1.5 text-[13px] whitespace-nowrap text-fg">
		<StatusDot status={d.status} />
		{statusMeta[d.status].label}
	</span>
{/snippet}

{#snippet metric(value: number | null, warn: number, crit: number)}
	{#if value === null}
		<span class="text-fg-faint">—</span>
	{:else}
		<span class="block w-20">
			<span class="mb-1 block num text-[12px] text-fg">{percent(value)}</span>
			<Meter {value} {warn} {crit} />
		</span>
	{/if}
{/snippet}

<ul class="divide-y divide-line md:hidden">
	{#each devices as d (d.id)}
		<li>
			<a
				href={href(d)}
				class="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-hover/50"
			>
				{@render icon(d)}
				<span class="min-w-0 flex-1">
					<span class="block truncate text-[14px] font-medium text-fg">{d.name}</span>
					<span class="block truncate font-mono text-[11px] text-fg-subtle">
						{osLabel(d.os)} · {pathLabel(d)}
					</span>
				</span>
				{@render status(d)}
			</a>
		</li>
	{/each}
</ul>

<div class="hidden overflow-x-auto md:block">
	<table class="w-full text-left text-[13px]">
		<thead>
			<tr class="border-b border-line text-[12px] text-fg-subtle">
				<th class="px-4 py-2.5 font-normal">Machine</th>
				<th class="px-3 py-2.5 font-normal">Status</th>
				<th class="px-3 py-2.5 font-normal">Connection</th>
				{#if detailed}
					<th class="px-3 py-2.5 font-normal">Tailnet IP</th>
				{/if}
				<th class="px-3 py-2.5 font-normal">CPU</th>
				<th class="px-3 py-2.5 font-normal">Memory</th>
				<th class="px-3 py-2.5 font-normal">Disk</th>
				<th class="px-4 py-2.5 text-right font-normal">Last seen</th>
			</tr>
		</thead>
		<tbody>
			{#each devices as d (d.id)}
				{@const m = d.metrics}
				<!-- The link stretches over the whole row, so any of it opens the device. -->
				<tr class="relative border-b border-line transition-colors last:border-0 hover:bg-hover/50">
					<td class="px-4 py-2.5">
						<span class="flex items-center gap-3">
							{@render icon(d)}
							<span class="min-w-0">
								<a
									href={href(d)}
									class="block truncate font-medium text-fg after:absolute after:inset-0 hover:underline"
								>
									{d.name}
								</a>
								<span class="block truncate font-mono text-[11px] text-fg-subtle">
									{osLabel(d.os)} · {shortName(d)}
								</span>
							</span>
						</span>
					</td>
					<td class="px-3 py-2.5">
						{@render status(d)}
						{#if d.status !== 'online'}
							<span
								class="block max-w-48 truncate text-[11.5px] text-fg-subtle"
								title={d.statusReason}
							>
								{d.statusReason}
							</span>
						{/if}
					</td>
					<td
						class={['px-3 py-2.5 num text-[12px]', slowLink(d) ? 'text-amber-fg' : 'text-fg-muted']}
					>
						{pathLabel(d)}
					</td>
					{#if detailed}
						<td class="px-3 py-2.5 num text-[12px] text-fg-muted">{tailnetIP(d) ?? '—'}</td>
					{/if}
					<td class="px-3 py-2.5">{@render metric(m?.cpu ?? null, 75, 90)}</td>
					<td class="px-3 py-2.5">
						{@render metric(ratio(m?.memory?.used, m?.memory?.total), 80, 92)}
					</td>
					<td class="px-3 py-2.5">
						{@render metric(ratio(m?.disk?.used, m?.disk?.total), 85, 95)}
					</td>
					<td class="px-4 py-2.5 text-right num text-[12px] text-fg-subtle">
						{d.online ? 'now' : ago(d.lastSeen, now)}
					</td>
				</tr>
			{/each}
		</tbody>
	</table>
</div>
