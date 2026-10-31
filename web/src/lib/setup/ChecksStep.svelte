<!--
	The first step of the setup: whether Roost can see what it needs to, and
	what to do about what it can't. The checks come from what the hub already
	knows, so they update on their own.
-->
<script lang="ts">
	import CircleCheck from '@lucide/svelte/icons/circle-check';
	import CircleDashed from '@lucide/svelte/icons/circle-dashed';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import type { Me, Snapshot, TailscaleMode } from '#lib/api/types.js';
	import { setupChecks } from '#lib/setup.js';
	import type { Status } from '#lib/status.js';
	import Card from '#lib/ui/Card.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';

	interface Props {
		snapshot: Snapshot | null;
		me: Me | null | undefined;
		mode: TailscaleMode | undefined;
	}

	let { snapshot, me, mode }: Props = $props();

	const checks = $derived(setupChecks(snapshot, me, mode));
	const attention = $derived(checks.filter((c) => c.status === 'attention').length);

	const icons: Partial<Record<Status, typeof CircleCheck>> = {
		online: CircleCheck,
		attention: TriangleAlert
	};
	const tones: Partial<Record<Status, string>> = {
		online: 'text-green-fg',
		attention: 'text-amber-fg'
	};
</script>

<Card
	title="Does Roost see what it needs to?"
	description={snapshot
		? attention === 0
			? 'Everything Roost needs is there. These update on their own.'
			: 'A few things need a look. These update on their own, so fix one and watch it turn green.'
		: 'Asking the hub…'}
>
	{#if !snapshot}
		<div class="space-y-3 p-4" aria-busy="true">
			<Skeleton class="h-12" />
			<Skeleton class="h-12" />
			<Skeleton class="h-12" />
		</div>
	{:else}
		<ul class="divide-y divide-line">
			{#each checks as c (c.id)}
				{@const Icon = icons[c.status] ?? CircleDashed}
				<li class="flex gap-3 px-4 py-3">
					<Icon class={['mt-0.5 size-4 shrink-0', tones[c.status] ?? 'text-fg-faint']} />
					<div class="min-w-0 flex-1">
						<p class="text-[14px] font-medium text-fg">{c.label}</p>
						<p class="mt-0.5 text-[13px] break-words text-fg-muted">{c.detail}</p>
						{#if c.hint}
							<p class="mt-1.5 text-[12.5px] text-fg-subtle">{c.hint}</p>
						{/if}
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</Card>
