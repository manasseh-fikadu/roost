<!--
	The processes in a container, as ps writes them on the machine, refreshed
	every few seconds while the container runs. Admins only; the page decides.
-->
<script lang="ts">
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import { plural } from '#lib/format.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import { getDockerProcesses } from '../api.js';
	import { Poll } from '../poll.svelte.js';

	interface Props {
		deviceId: string;
		/** The container's name. */
		container: string;
		/** Whether the container runs and isn't paused, which is when it has processes to list. */
		running: boolean;
	}

	let { deviceId, container, running }: Props = $props();

	/** The columns of numbers, which line up on the right. */
	const numeric = new Set(['PID', 'PPID', '%CPU', '%MEM', 'VSZ', 'RSS', 'C']);

	const procs = new Poll(() => getDockerProcesses(deviceId, container), 5000);

	$effect(() => {
		if (!running) {
			return;
		}
		return procs.start();
	});

	const data = $derived(procs.data);
	const commandColumn = $derived(
		data ? Math.max(data.titles.indexOf('COMMAND'), data.titles.indexOf('CMD')) : -1
	);
</script>

{#if !running}
	<Card>
		<EmptyState title="Not running">Processes show up while the container runs.</EmptyState>
	</Card>
{:else}
	<Card
		title="Processes"
		description={data
			? `${plural(data.processes.length, 'process', 'processes')} · refreshes every 5 seconds · PIDs are as the machine sees them`
			: 'From ps on the machine'}
	>
		{#snippet actions()}
			<IconButton label="Refresh" onclick={() => procs.reload()}>
				<RefreshCw class="size-3.5" />
			</IconButton>
		{/snippet}
		{#if procs.error}
			<p
				role="alert"
				class="border-b border-amber/25 bg-amber/5 px-4 py-2 text-[13px] break-words text-amber-fg"
			>
				{data ? 'Refresh failed: ' : ''}{procs.error}
			</p>
		{/if}
		{#if !data}
			{#if !procs.error}
				<div class="p-4"><Skeleton class="h-24" /></div>
			{/if}
		{:else if data.processes.length === 0}
			<EmptyState title="No processes" />
		{:else}
			<div class="max-h-[70dvh] overflow-auto">
				<table class="w-full text-left text-[12.5px]">
					<thead>
						<tr class="text-[12px] text-fg-subtle">
							{#each data.titles as t, j (j)}
								<th
									class={[
										'sticky top-0 z-10 bg-surface px-3 py-2.5 font-normal whitespace-nowrap shadow-[inset_0_-1px_0_var(--color-line)] first:pl-4 last:pr-4',
										numeric.has(t) && 'text-right'
									]}
								>
									{t}
								</th>
							{/each}
						</tr>
					</thead>
					<tbody>
						{#each data.processes as p, i (i)}
							<tr class="border-b border-line last:border-0">
								{#each p as cell, j (j)}
									<td
										class={[
											'px-3 py-2 align-top first:pl-4 last:pr-4',
											j === commandColumn
												? 'min-w-64 font-mono text-[12px] break-all text-fg'
												: 'num whitespace-nowrap text-fg-muted',
											numeric.has(data.titles[j] ?? '') && 'text-right'
										]}
									>
										{cell}
									</td>
								{/each}
							</tr>
						{/each}
					</tbody>
				</table>
			</div>
		{/if}
	</Card>
{/if}
