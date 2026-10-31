<!-- A container's ports: the ones published on the machine, and the ones only exposed. -->
<script lang="ts">
	import type { ContainerPort } from '#lib/api/types.js';
	import Card from '#lib/ui/Card.svelte';
	import DataRow from '#lib/ui/DataRow.svelte';
	import { portName, splitPorts } from './helpers.js';

	interface Props {
		ports: ContainerPort[];
	}

	let { ports }: Props = $props();

	const split = $derived(splitPorts(ports));
</script>

<Card title="Ports">
	{#if ports.length === 0}
		<p class="px-4 py-3 text-[13px] text-fg-subtle">No ports.</p>
	{:else}
		<dl>
			{#each split.published as p, i (`${p.ip ?? ''}:${p.public}/${p.type}/${i}`)}
				<DataRow label={portName(p)} copy={String(p.public)}>
					{p.ip ?? '0.0.0.0'}:{p.public}
				</DataRow>
			{/each}
			{#each split.exposed as p, i (`${portName(p)}/${i}`)}
				<DataRow label={portName(p)}>
					<span class="text-fg-subtle">not published</span>
				</DataRow>
			{/each}
		</dl>
	{/if}
</Card>
