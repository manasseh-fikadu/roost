<!--
	What is mounted into a container: its type, where it comes from (a bind
	mount opens in Files for admins, and a volume shows its size when Docker has
	measured it), where it goes, and whether it can be written.
-->
<script lang="ts">
	import FolderOpen from '@lucide/svelte/icons/folder-open';
	import { resolve } from '$app/paths';
	import type { DockerMount } from '#lib/api/types.js';
	import { bytes } from '#lib/format.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import { volumeName } from './helpers.js';

	interface Props {
		mounts: DockerMount[];
		deviceId: string;
		/** Whether a bind mount can open in Files. */
		canBrowse: boolean;
	}

	let { mounts, deviceId, canBrowse }: Props = $props();

	function filesUrl(path: string): string {
		return `${resolve('/files')}?device=${encodeURIComponent(deviceId)}&path=${encodeURIComponent(path)}`;
	}
</script>

{#if mounts.length === 0}
	<Card>
		<EmptyState title="No mounts">
			Everything this container writes stays in its own writable layer.
		</EmptyState>
	</Card>
{:else}
	<Card
		title="Mounts"
		description="Volume sizes come from Docker's disk usage scan, which it runs now and then."
	>
		<ul>
			{#each mounts as m (m.destination)}
				<li
					class="grid gap-x-4 gap-y-1.5 border-b border-line px-4 py-3 last:border-0 md:grid-cols-[88px_minmax(0,1.3fr)_minmax(0,1fr)_96px] md:items-center"
				>
					<div>
						<Badge mono tone={m.type === 'volume' ? 'purple' : m.type === 'bind' ? 'blue' : 'gray'}>
							{m.type.toUpperCase()}
						</Badge>
					</div>
					<div class="min-w-0">
						{#if m.type === 'bind'}
							{#if canBrowse}
								<a
									href={filesUrl(m.source)}
									title="Open in Files"
									class="group flex min-w-0 items-center gap-1.5 font-mono text-[12.5px] text-fg hover:underline"
								>
									<FolderOpen class="size-3.5 shrink-0 text-fg-subtle group-hover:text-fg" />
									<span class="truncate">{m.source}</span>
								</a>
							{:else}
								<span class="block truncate font-mono text-[12.5px] text-fg" title={m.source}>
									{m.source}
								</span>
							{/if}
						{:else if m.type === 'volume'}
							<div class="flex min-w-0 items-center gap-2">
								<span class="truncate font-mono text-[12.5px] text-fg" title={m.name}>
									{volumeName(m.name)}
								</span>
								{#if m.size != null}
									<span class="shrink-0 num text-[12px] text-fg-muted">{bytes(m.size)}</span>
								{/if}
							</div>
							<div class="truncate font-mono text-[11px] text-fg-faint" title={m.source}>
								{m.source}
							</div>
						{:else}
							<span class="font-mono text-[12.5px] text-fg-muted">{m.source || m.type}</span>
						{/if}
					</div>
					<div class="flex min-w-0 items-center gap-1.5 font-mono text-[12.5px]">
						<span class="text-fg-faint" aria-hidden="true">→</span>
						<span class="truncate text-fg" title={m.destination}>{m.destination}</span>
					</div>
					<div class="flex items-center gap-1.5 md:justify-end">
						<Badge tone={m.rw ? 'amber' : 'gray'} mono>{m.rw ? 'RW' : 'RO'}</Badge>
						{#if m.propagation && m.propagation !== 'rprivate'}
							<Badge mono>{m.propagation}</Badge>
						{/if}
					</div>
				</li>
			{/each}
		</ul>
	</Card>
{/if}
