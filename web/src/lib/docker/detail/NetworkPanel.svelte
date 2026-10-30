<!--
	A container's networking: one card for each network it is on, its ports, and
	its network mode, hostname, DNS servers and extra hosts.
-->
<script lang="ts">
	import type { DockerContainerDetail } from '#lib/api/types.js';
	import Card from '#lib/ui/Card.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import { shortId } from '../docker.js';
	import PortsCard from './PortsCard.svelte';

	interface Props {
		container: DockerContainerDetail;
	}

	let { container: d }: Props = $props();
</script>

{#snippet list(items: string[])}
	<span class="block font-mono text-[12px] whitespace-normal">{items.join(', ')}</span>
{/snippet}

<div class="grid grid-cols-[minmax(0,1fr)] gap-4 xl:grid-cols-[minmax(0,1fr)_380px]">
	<div class="min-w-0 space-y-4">
		{#if d.networks.length === 0}
			<Card>
				<EmptyState title="No networks">Network mode: {d.networkMode || 'none'}</EmptyState>
			</Card>
		{/if}
		{#each d.networks as n (n.name)}
			{@const ipv4 = n.ip ? `${n.ip}${n.prefixLen ? `/${n.prefixLen}` : ''}` : ''}
			<Card title={n.name} description={shortId(n.id)}>
				<dl>
					<DataRow label="IPv4" copy={n.ip || undefined}>{ipv4 || '—'}</DataRow>
					<DataRow label="Gateway" copy={n.gateway || undefined}>{n.gateway || '—'}</DataRow>
					{#if n.ipv6}
						<DataRow label="IPv6" copy={n.ipv6}>{n.ipv6}</DataRow>
					{/if}
					{#if n.ipv6Gateway}
						<DataRow label="IPv6 gateway">{n.ipv6Gateway}</DataRow>
					{/if}
					<DataRow label="MAC" copy={n.mac || undefined}>{n.mac || '—'}</DataRow>
					<DataRow label="DNS names">
						{#if n.dnsNames.length}{@render list(n.dnsNames)}{:else}—{/if}
					</DataRow>
					{#if n.aliases.length > 0}
						<DataRow label="Aliases">{@render list(n.aliases)}</DataRow>
					{/if}
				</dl>
			</Card>
		{/each}
	</div>
	<div class="min-w-0 space-y-4">
		<PortsCard ports={d.ports} />
		<Card title="Settings">
			<dl>
				<DataRow label="Network mode">{d.networkMode || '—'}</DataRow>
				<DataRow label="Hostname" copy={d.hostname || undefined}>{d.hostname || '—'}</DataRow>
				<DataRow label="DNS servers">
					{#if d.dns.length}{@render list(d.dns)}{:else}default{/if}
				</DataRow>
				<DataRow label="Extra hosts">
					{#if d.extraHosts.length}{@render list(d.extraHosts)}{:else}—{/if}
				</DataRow>
			</dl>
		</Card>
	</div>
</div>
