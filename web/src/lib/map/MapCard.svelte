<!--
	A card on the network map: an icon, a title, a status, a line about it, and
	a grid of <MapFact>s under that. It's a link if `href` is given. It fills
	the box the map gives it.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { Icon } from '#lib/icon.js';
	import type { Status } from '#lib/status.js';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import StatusPill from '#lib/ui/StatusPill.svelte';

	interface Props {
		icon: Icon;
		title: string;
		status: Status;
		subtitle: string;
		href?: string;
		/** Picks the card out, as the hub's is. */
		highlight?: boolean;
		/** The <MapFact>s. */
		children?: Snippet;
	}

	let {
		icon: IconComponent,
		title,
		status,
		subtitle,
		href,
		highlight = false,
		children
	}: Props = $props();
</script>

<svelte:element
	this={href ? 'a' : 'div'}
	{href}
	class={[
		'group flex h-full flex-col rounded-lg border bg-surface p-3 transition-colors duration-150',
		highlight ? 'border-line-strong shadow-[0_0_0_4px_rgb(255_255_255/0.02)]' : 'border-line',
		href && 'hover:border-line-heavy'
	]}
>
	<div class="flex items-start gap-2.5">
		<div
			class="flex size-7 shrink-0 items-center justify-center rounded-md border border-line bg-raised text-fg-muted group-hover:text-fg"
		>
			<IconComponent class="size-4" strokeWidth={1.6} />
		</div>
		<div class="min-w-0 flex-1">
			<div class="flex items-center gap-2">
				<span class="truncate text-[13px] leading-4 font-medium text-fg">{title}</span>
				<StatusDot {status} pulse class="ml-auto" />
			</div>
			<div class="mt-1.5 flex min-w-0 items-center gap-1.5">
				<StatusPill {status} />
				<span class="truncate text-[11.5px] text-fg-subtle" title={subtitle}>{subtitle}</span>
			</div>
		</div>
	</div>
	{#if children}
		<dl class="mt-auto grid grid-cols-2 gap-x-3 gap-y-1 pt-2.5">{@render children()}</dl>
	{/if}
</svelte:element>
