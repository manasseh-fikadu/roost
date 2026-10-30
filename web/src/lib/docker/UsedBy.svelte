<!--
	The containers that use a volume or an image, or are on a network, as links
	to their pages, each with a dot for whether it runs. A thing that no
	container uses says so.
-->
<script lang="ts">
	import type { DockerRef } from '#lib/api/types.js';
	import Badge from '#lib/ui/Badge.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import { containerHref } from './containerLink.js';

	interface Props {
		refs: (DockerRef & { ip?: string })[];
		deviceId: string;
		/** What to say if there are none. */
		empty?: string;
		/** Say it with a badge, not in plain text. */
		badge?: boolean;
	}

	let { refs, deviceId, empty = 'unused', badge = false }: Props = $props();
</script>

{#if refs.length === 0}
	{#if badge}
		<Badge tone="amber">{empty}</Badge>
	{:else}
		<span class="text-[12px] text-fg-faint">{empty}</span>
	{/if}
{:else}
	<ul class="flex min-w-0 flex-wrap gap-x-3 gap-y-1">
		{#each refs as r (r.id)}
			<li class="flex min-w-0 items-center gap-1.5">
				<StatusDot status={r.running ? 'online' : 'unknown'} />
				<a
					href={containerHref(deviceId, r.name)}
					class={[
						'truncate text-[12.5px] hover:underline',
						r.running ? 'text-fg' : 'text-fg-subtle'
					]}
				>
					{r.name}<span class="sr-only">{r.running ? ' (running)' : ' (stopped)'}</span>
				</a>
				{#if r.ip}
					<span class="font-mono text-[11px] text-fg-faint">{r.ip}</span>
				{/if}
			</li>
		{/each}
	</ul>
{/if}
