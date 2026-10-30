<!--
	Docker's numbers on one device in tiles: the containers, what the running
	ones use together, the disk and the engine. Without the list of
	containers yet, it holds their places.
-->
<script lang="ts">
	import type { DockerContainerRow, DockerOverview } from '#lib/api/types.js';
	import MetricTile from '#lib/devices/MetricTile.svelte';
	import { bytes, percent, plural, rate, ratio } from '#lib/format.js';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import { totals } from './docker.js';
	import { cpuMeter, reclaimable } from './list.js';

	interface Props {
		/** Every container on the device, or null before the list has loaded. */
		containers: DockerContainerRow[] | null;
		/** The daemon, or null before it has answered. */
		overview: DockerOverview | null;
	}

	let { containers, overview }: Props = $props();

	const sum = $derived(containers ? totals(containers) : null);
	// The daemon knows how many cores the machine has; a container's stats know how many it sees.
	const cpus = $derived(overview?.cpus || sum?.cpus || null);
	const disk = $derived(overview?.disk ?? null);
	const stoppedLine = $derived(
		sum
			? `${sum.stopped} stopped · ${sum.unhealthy > 0 ? `${sum.unhealthy} unhealthy` : 'none unhealthy'}`
			: ''
	);
</script>

{#if !sum}
	<div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6" aria-hidden="true">
		{#each { length: 6 }, i (i)}
			<Skeleton class="h-[104px] rounded-xl" />
		{/each}
	</div>
{:else}
	<div class="grid grid-cols-2 gap-3 md:grid-cols-3 xl:grid-cols-6">
		<MetricTile label="Containers running" value="{sum.running} / {sum.total}" sub={stoppedLine} />
		<MetricTile
			label="CPU"
			value={percent(sum.cpu, 1)}
			meter={cpuMeter(sum.cpu, cpus) ?? undefined}
			warn={75}
			crit={90}
			sub={cpus ? `${(sum.cpu / 100).toFixed(2)} of ${plural(cpus, 'core')}` : 'across containers'}
		/>
		<MetricTile
			label="Memory"
			value={bytes(sum.mem)}
			meter={ratio(sum.mem, overview?.memTotal) ?? undefined}
			sub={overview ? `of ${bytes(overview.memTotal)} on the host` : 'across containers'}
		/>
		<MetricTile label="Network" value="↓ {rate(sum.rx)}" sub="↑ {rate(sum.tx)}" />
		{#if disk}
			<MetricTile
				label="Disk"
				value={bytes(disk.total)}
				sub={reclaimable(disk) > 0
					? `${bytes(reclaimable(disk))} reclaimable`
					: `${plural(disk.images.count, 'image')} · ${plural(disk.volumes.count, 'volume')}`}
			/>
		{:else}
			<MetricTile
				label="Disk"
				value={overview ? 'scanning…' : '—'}
				small={!!overview}
				sub="images, containers, volumes"
			/>
		{/if}
		<MetricTile
			label="Engine"
			value={overview?.version ?? '—'}
			sub={overview
				? `${overview.storageDriver}${overview.apiVersion ? ` · API ${overview.apiVersion}` : ''}`
				: undefined}
		/>
	</div>
{/if}
