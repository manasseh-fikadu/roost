<!-- The networks on a device, with their address ranges and the containers on each. -->
<script lang="ts">
	import Network from '@lucide/svelte/icons/network';
	import { plural } from '#lib/format.js';
	import Badge from '#lib/ui/Badge.svelte';
	import { getDockerNetworks } from './api.js';
	import { networkTotals } from './list.js';
	import { Poll } from './poll.svelte.js';
	import ResourceCard from './ResourceCard.svelte';
	import UsedBy from './UsedBy.svelte';

	interface Props {
		deviceId: string;
	}

	let { deviceId }: Props = $props();

	// A new poll for another device, which the effect starts and the old one's stop.
	const poll = $derived.by(() => {
		const id = deviceId;
		return new Poll(() => getDockerNetworks(id), 30_000);
	});
	$effect(() => poll.start());

	const totals = $derived(poll.data ? networkTotals(poll.data) : null);
	const description = $derived(
		totals
			? `${plural(totals.count, 'network')} · ${totals.inUse} in use`
			: 'Networks that containers can join'
	);

	const th = 'px-3 py-2.5 font-normal first:pl-4 last:pr-4';
	const td = 'px-3 py-2.5 align-top first:pl-4 last:pr-4';
</script>

<ResourceCard title="Networks" {description} icon={Network} empty="No networks" {poll}>
	{#snippet children(networks)}
		<div class="overflow-x-auto">
			<table class="w-full min-w-208 text-left text-[13px]">
				<thead>
					<tr class="border-b border-line text-[12px] text-fg-subtle">
						<th scope="col" class={th}>Network</th>
						<th scope="col" class={th}>Driver</th>
						<th scope="col" class={th}>Scope</th>
						<th scope="col" class={th}>Subnets</th>
						<th scope="col" class={th}>Containers</th>
					</tr>
				</thead>
				<tbody>
					{#each networks as n (n.id)}
						<tr class="border-b border-line transition-colors last:border-0 hover:bg-hover/40">
							<td class="{td} w-[26%] max-w-0">
								<span class="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1">
									<span class="truncate text-[13.5px] font-medium text-fg" title={n.name}>
										{n.name}
									</span>
									{#if n.internal}
										<Badge tone="purple">internal</Badge>
									{/if}
									{#if n.ipv6}
										<Badge tone="blue">IPv6</Badge>
									{/if}
									{#if n.attachable}
										<Badge>attachable</Badge>
									{/if}
								</span>
								{#if n.project}
									<span class="block truncate text-[11.5px] text-fg-subtle">
										Compose project {n.project}
									</span>
								{/if}
							</td>
							<td class="{td} text-[12.5px] text-fg-muted">{n.driver}</td>
							<td class="{td} text-[12.5px] text-fg-muted">{n.scope}</td>
							<td class="{td} font-mono text-[12px] whitespace-nowrap text-fg-muted">
								{#each n.subnets as s (s.subnet)}
									<span class="block">
										{s.subnet}
										{#if s.gateway}
											<span class="text-fg-faint">via {s.gateway}</span>
										{/if}
									</span>
								{:else}
									<span class="font-sans text-[12px] text-fg-faint">none</span>
								{/each}
							</td>
							<td class="{td} min-w-48">
								<UsedBy refs={n.containers} {deviceId} empty="no containers" />
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</ResourceCard>
