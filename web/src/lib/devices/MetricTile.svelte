<!--
	One of a device's metrics: its name, its value, a meter if it has one, and
	a line under it. With `onclick`, it's a button that says what uses the
	metric.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import ChevronRight from '@lucide/svelte/icons/chevron-right';
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
		/** Drawn before the value, such as an icon. */
		icon?: Snippet;
		/** Opens a breakdown of what uses the metric. */
		onclick?: () => void;
	}

	let { label, value, small = false, sub, meter, warn, crit, icon, onclick }: Props = $props();
</script>

{#snippet body()}
	<div class="flex items-center justify-between gap-2 text-[12px] text-fg-subtle">
		<span>{label}</span>
		{#if onclick}
			<span
				class="flex items-center gap-0.5 text-[11px] text-fg-faint transition-colors group-hover:text-fg-muted"
			>
				<span
					class="hidden opacity-0 transition-opacity group-hover:opacity-100 group-focus-visible:opacity-100 sm:inline"
					>What's using it</span
				>
				<ChevronRight class="size-3.5" />
			</span>
		{/if}
	</div>
	<div
		class={[
			'mt-1 flex items-center gap-2 truncate num font-medium tracking-[-0.02em] text-fg',
			small ? 'text-[16px] leading-[33px]' : 'text-[22px]'
		]}
	>
		{@render icon?.()}
		{value}
	</div>
	{#if meter !== undefined}
		<Meter value={meter} {warn} {crit} class="mt-2" />
	{/if}
	{#if sub}
		<div class="mt-1.5 truncate text-[12px] text-fg-subtle">{sub}</div>
	{/if}
{/snippet}

{#if onclick}
	<button
		type="button"
		{onclick}
		aria-label="{label}: {value}. See what's using it"
		class="group card p-4 text-left transition-colors hover:border-line-strong hover:bg-raised"
	>
		{@render body()}
	</button>
{:else}
	<div class="card p-4">
		{@render body()}
	</div>
{/if}
