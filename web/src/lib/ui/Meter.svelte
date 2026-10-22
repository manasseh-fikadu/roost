<!--
	A thin bar of how much of something is used, amber from `warn` and red from
	`crit`. It's decoration: show the number in words too.
-->
<script lang="ts">
	interface Props {
		/** A percentage, or null if it's not known. */
		value: number | null;
		warn?: number;
		crit?: number;
		class?: string;
	}

	let { value, warn = 80, crit = 92, class: className }: Props = $props();

	const pct = $derived(value === null ? 0 : Math.max(0, Math.min(100, value)));
	const color = $derived(
		value === null
			? 'bg-line-strong'
			: pct >= crit
				? 'bg-red-fg'
				: pct >= warn
					? 'bg-amber'
					: 'bg-fg'
	);
</script>

<span
	class={['block h-1 w-full overflow-hidden rounded-full bg-line-strong/70', className]}
	aria-hidden="true"
>
	<span
		class={['block h-full rounded-full transition-[width] duration-500', color]}
		style:width="{pct}%"
	></span>
</span>
