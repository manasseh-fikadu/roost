<!--
	A card of the alerts that need attention, compact, for Overview and a
	device's page, with a link to the rest.
-->
<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import ShieldCheck from '@lucide/svelte/icons/shield-check';
	import { resolve } from '$app/paths';
	import { activeAlerts } from '#lib/alerts.js';
	import type { Alert, Device } from '#lib/api/types.js';
	import { plural } from '#lib/format.js';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import AlertCard from './AlertCard.svelte';

	interface Props {
		/** The alerts to show from, or null before the first snapshot. */
		alerts: Alert[] | null;
		devices: Device[];
		now: number;
		/** The most to show. */
		limit?: number;
		/** What the empty card says. */
		empty?: string;
		class?: string;
	}

	let {
		alerts,
		devices,
		now,
		limit = 4,
		empty = 'Everything the hub can see looks fine.',
		class: className
	}: Props = $props();

	const active = $derived(alerts ? activeAlerts(alerts) : []);
	const snoozed = $derived(alerts ? alerts.filter((a) => a.state === 'snoozed').length : 0);
	const shown = $derived(active.slice(0, limit));
</script>

<Card class={className}>
	<header class="flex items-center gap-2 border-b border-line px-4 py-3">
		<h2 class="text-[14px] leading-5 font-medium text-fg">Alerts</h2>
		{#if active.length > 0}
			<span class="rounded-full bg-amber/15 px-1.5 num text-[11px] leading-[18px] text-amber-fg">
				{active.length}
			</span>
		{/if}
		<a
			href={resolve('/alerts')}
			class="ml-auto flex items-center gap-1 text-[12px] text-fg-subtle transition-colors hover:text-fg"
		>
			All alerts <ArrowRight class="size-3" />
		</a>
	</header>
	{#if alerts === null}
		<div class="space-y-2 p-3">
			<Skeleton class="h-16" />
		</div>
	{:else if shown.length === 0}
		<EmptyState title="No open alerts" icon={ShieldCheck}>{empty}</EmptyState>
	{:else}
		<div class="grid gap-2 p-3">
			{#each shown as a (a.id)}
				<AlertCard alert={a} device={devices.find((d) => d.id === a.deviceId)} {now} compact />
			{/each}
		</div>
	{/if}
	{#if active.length > shown.length || snoozed > 0}
		<p class="border-t border-line px-4 py-2.5 text-[12px] text-fg-subtle">
			{#if active.length > shown.length}
				<a href={resolve('/alerts')} class="hover:text-fg">
					{plural(active.length - shown.length, 'more alert')}
				</a>
			{/if}
			{#if active.length > shown.length && snoozed > 0}
				·
			{/if}
			{#if snoozed > 0}
				<a href="{resolve('/alerts')}?tab=snoozed" class="hover:text-fg">{snoozed} snoozed</a>
			{/if}
		</p>
	{/if}
</Card>
