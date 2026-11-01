<!--
	What runs on the machine Bield runs on: the systemd units it watches and
	the ones that failed, and how many containers there are.
-->
<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { resolve } from '$app/paths';
	import { unitStatus } from '#lib/alerts.js';
	import type { Container, Unit } from '#lib/api/types.js';
	import { ago, bytes, plural } from '#lib/format.js';
	import { statusMeta } from '#lib/status.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Card from '#lib/ui/Card.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	interface Props {
		units: Unit[];
		containers: Container[];
		now: number;
	}

	let { units, containers, now }: Props = $props();

	const running = $derived(containers.filter((c) => c.state === 'running').length);
</script>

<Card title="Services" description="systemd units Bield watches, and any that failed">
	{#if units.length === 0}
		<p class="px-4 py-4 text-[13px] text-fg-subtle">No watched units on this machine.</p>
	{:else}
		<ul class="divide-y divide-line">
			{#each units as u (u.scope + u.name)}
				{@const status = unitStatus(u)}
				<li class="flex items-start gap-3 px-4 py-2.5">
					<StatusDot {status} class="mt-1.5" />
					<div class="min-w-0 flex-1">
						<div class="flex items-center gap-2">
							<span class="truncate font-mono text-[12.5px] text-fg">{u.name}</span>
							{#if u.scope === 'user'}
								<Badge>user</Badge>
							{/if}
						</div>
						{#if u.description}
							<p class="truncate text-[12px] text-fg-subtle" title={u.description}>
								{u.description}
							</p>
						{/if}
					</div>
					<div class="shrink-0 text-right">
						<p class={['text-[12px]', statusMeta[status].text]}>{u.active} · {u.sub}</p>
						<p class="num text-[11px] text-fg-faint">
							{#if u.since}{ago(u.since, now)}{/if}{#if u.memory}{u.since ? ' · ' : ''}{bytes(
									u.memory
								)}{/if}
						</p>
					</div>
				</li>
			{/each}
		</ul>
	{/if}
	{#if containers.length > 0}
		<a
			href={resolve('/docker')}
			class="flex items-center justify-between gap-2 border-t border-line px-4 py-2.5 text-[12px] text-fg-subtle transition-colors hover:text-fg"
		>
			<span>Docker: {running} of {plural(containers.length, 'container')} running</span>
			<ArrowRight class="size-3" />
		</a>
	{/if}
</Card>
