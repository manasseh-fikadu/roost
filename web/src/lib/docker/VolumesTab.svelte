<!-- The volumes on a device, with what each holds and which containers use it. -->
<script lang="ts">
	import HardDrive from '@lucide/svelte/icons/hard-drive';
	import { ago, bytes, plural } from '#lib/format.js';
	import Badge from '#lib/ui/Badge.svelte';
	import { getDockerVolumes } from './api.js';
	import { volumeName, volumeTotals } from './list.js';
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
		return new Poll(() => getDockerVolumes(id), 30_000);
	});
	$effect(() => poll.start());

	const totals = $derived(poll.data ? volumeTotals(poll.data) : null);
	const description = $derived(
		totals
			? [
					plural(totals.count, 'volume'),
					totals.measured ? bytes(totals.size) : null,
					totals.unused > 0 ? `${totals.unused} unused` : null
				]
					.filter(Boolean)
					.join(' · ')
			: 'Named and anonymous volumes'
	);

	const th = 'px-3 py-2.5 font-normal first:pl-4 last:pr-4';
	const td = 'px-3 py-2.5 align-top first:pl-4 last:pr-4';
</script>

<ResourceCard title="Volumes" {description} icon={HardDrive} empty="No volumes" {poll}>
	{#snippet children(volumes)}
		<div class="overflow-x-auto">
			<table class="w-full min-w-176 text-left text-[13px]">
				<thead>
					<tr class="border-b border-line text-[12px] text-fg-subtle">
						<th scope="col" class={th}>Volume</th>
						<th scope="col" class={th}>Driver</th>
						<th scope="col" class={th}>Size</th>
						<th scope="col" class={th}>Used by</th>
						<th scope="col" class={th}>Project</th>
						<th scope="col" class="{th} text-right">Created</th>
					</tr>
				</thead>
				<tbody>
					{#each volumes as v (v.name)}
						<tr class="border-b border-line transition-colors last:border-0 hover:bg-hover/40">
							<td class="{td} w-[30%] max-w-0">
								<span class="block truncate font-mono text-[12.5px] text-fg" title={v.name}>
									{volumeName(v.name)}
								</span>
								{#if v.mountpoint}
									<span
										class="block truncate font-mono text-[11px] text-fg-faint"
										title={v.mountpoint}
									>
										{v.mountpoint}
									</span>
								{/if}
							</td>
							<td class="{td} text-[12.5px] text-fg-muted">{v.driver}</td>
							<td class="{td} num text-[12.5px] whitespace-nowrap">
								{#if v.size != null}
									{bytes(v.size)}
								{:else}
									<span class="text-fg-faint" title="Docker hasn't measured it yet">—</span>
								{/if}
							</td>
							<td class="{td} min-w-40">
								<UsedBy refs={v.usedBy} {deviceId} badge />
							</td>
							<td class={td}>
								{#if v.project}
									<Badge mono>{v.project}</Badge>
								{:else}
									<span class="text-fg-faint">—</span>
								{/if}
							</td>
							<td class="{td} text-right num text-[12px] whitespace-nowrap text-fg-subtle">
								{v.created ? ago(v.created) : '—'}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/snippet}
</ResourceCard>
