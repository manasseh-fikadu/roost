<!-- The images on a device, with how big each is and which containers use it. -->
<script lang="ts">
	import ImageIcon from '@lucide/svelte/icons/image';
	import { ago, bytes, plural } from '#lib/format.js';
	import Badge from '#lib/ui/Badge.svelte';
	import CopyButton from '#lib/ui/CopyButton.svelte';
	import { getDockerImages } from './api.js';
	import { shortId } from './docker.js';
	import { imageTotals, shortDigest } from './list.js';
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
		return new Poll(() => getDockerImages(id), 30_000);
	});
	$effect(() => poll.start());

	const totals = $derived(poll.data ? imageTotals(poll.data) : null);
	const description = $derived(
		totals
			? [
					`${plural(totals.count, 'image')} · ${bytes(totals.size)}`,
					totals.unused > 0 ? `${totals.unused} unused` : null,
					totals.dangling > 0 ? `${totals.dangling} dangling` : null
				]
					.filter(Boolean)
					.join(' · ')
			: 'Images on the machine'
	);

	const th = 'px-3 py-2.5 font-normal first:pl-4 last:pr-4';
	const td = 'px-3 py-2.5 align-top first:pl-4 last:pr-4';
</script>

<ResourceCard title="Images" {description} icon={ImageIcon} empty="No images" {poll}>
	{#snippet children(images)}
		<div class="overflow-x-auto">
			<table class="w-full min-w-176 text-left text-[13px]">
				<thead>
					<tr class="border-b border-line text-[12px] text-fg-subtle">
						<th scope="col" class={th}>Image</th>
						<th scope="col" class={th}>ID</th>
						<th scope="col" class={th}>Size</th>
						<th scope="col" class={th}>Used by</th>
						<th scope="col" class="{th} text-right">Created</th>
					</tr>
				</thead>
				<tbody>
					{#each images as i (i.id)}
						{@const digest = shortDigest(i.digests[0])}
						<tr class="border-b border-line transition-colors last:border-0 hover:bg-hover/40">
							<td class="{td} w-[32%] max-w-0">
								<span class="flex min-w-0 items-center gap-2">
									{#if i.tags.length > 0}
										<span
											class="truncate font-mono text-[12.5px] font-medium text-fg"
											title={i.tags[0]}
										>
											{i.tags[0]}
										</span>
									{:else}
										<span class="font-mono text-[12.5px] text-fg-subtle">&lt;none&gt;</span>
									{/if}
									{#if i.dangling}
										<Badge tone="amber">dangling</Badge>
									{/if}
								</span>
								{#if i.tags.length > 1}
									<span
										class="block truncate font-mono text-[11px] text-fg-subtle"
										title={i.tags.slice(1).join('\n')}
									>
										{i.tags.slice(1).join(', ')}
									</span>
								{/if}
								{#if digest}
									<span
										class="block truncate font-mono text-[11px] text-fg-faint"
										title={i.digests[0]}
									>
										{digest}…
									</span>
								{/if}
							</td>
							<td class={td}>
								<span class="flex items-center gap-1 font-mono text-[12px] text-fg-muted">
									{shortId(i.id)}
									<CopyButton value={i.id} label="Copy image ID" />
								</span>
							</td>
							<td class="{td} num text-[12.5px] whitespace-nowrap">{bytes(i.size)}</td>
							<td class="{td} min-w-40">
								<UsedBy refs={i.usedBy} {deviceId} />
							</td>
							<td class="{td} text-right num text-[12px] whitespace-nowrap text-fg-subtle">
								{ago(i.created)}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</ResourceCard>
