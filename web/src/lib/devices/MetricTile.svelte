<!-- One of a device's metrics: its name, its value, a meter if it has one, and a line under it. -->
<script lang="ts">
	import Meter from '#lib/ui/Meter.svelte';

	interface Props {
		label: string;
		value: string;
		/** A smaller value, for longer text. */
		small?: boolean;
		sub?: string;
		/** How much is used, in percent, for a meter under the value. */
		meter?: number | null;
		warn?: number;
		crit?: number;
	}

	let { label, value, small = false, sub, meter, warn, crit }: Props = $props();
</script>

<div class="card p-4">
	<div class="text-[12px] text-fg-subtle">{label}</div>
	<div
		class={[
			'mt-1 truncate num font-medium tracking-[-0.02em] text-fg',
			small ? 'text-[16px] leading-[33px]' : 'text-[22px]'
		]}
	>
		{value}
	</div>
	{#if meter !== undefined}
		<Meter value={meter} {warn} {crit} class="mt-2" />
	{/if}
	{#if sub}
		<div class="mt-1.5 truncate text-[12px] text-fg-subtle">{sub}</div>
	{/if}
</div>
