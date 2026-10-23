<!--
	A dialog about one check: how it's doing, its latency and uptime over
	time, and its latest results.
-->
<script lang="ts">
	import ArrowRight from '@lucide/svelte/icons/arrow-right';
	import { resolve } from '$app/paths';
	import type { Check, CheckResult } from '#lib/api/types.js';
	import { checkStatus, every, kindLabels, stateLabels, targetOf } from '#lib/checks.js';
	import { dateTime, latency, percent } from '#lib/format.js';
	import { historyRanges, type HistoryRange } from '#lib/history.js';
	import { statusMeta } from '#lib/status.js';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Segmented from '#lib/ui/Segmented.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import CheckHistory from './CheckHistory.svelte';

	interface Props {
		check: Check;
		/** The check's latest results, oldest first. */
		recent: CheckResult[];
		open?: boolean;
	}

	let { check, recent, open = $bindable(false) }: Props = $props();

	let range: HistoryRange = $state('24h');

	const last = $derived(check.last);
	const tiles = $derived([
		{ label: 'Latest', value: latency(last?.latencyMs) },
		{ label: 'Uptime, 24h', value: percent(check.uptime, 2) },
		{ label: 'Runs', value: every(check.intervalSec) }
	]);
	// The newest first.
	const results = $derived(recent.toReversed().slice(0, 12));
</script>

<Dialog
	bind:open
	title={check.name}
	description="{kindLabels[check.kind]} · {targetOf(check)}"
	width="sm:max-w-2xl"
>
	<div class="space-y-5">
		<div class="flex items-start gap-3 rounded-lg border border-line bg-canvas px-3 py-2.5">
			<StatusDot status={checkStatus[check.state]} class="mt-1.5" />
			<div class="min-w-0">
				<p class={['text-[14px] font-medium', statusMeta[checkStatus[check.state]].text]}>
					{stateLabels[check.state]}
				</p>
				<p class="text-[13px] break-words text-fg-subtle">{check.stateReason}</p>
			</div>
			{#if check.deviceId}
				<a
					href={resolve('/devices/[id]', { id: check.deviceId })}
					class="ml-auto flex shrink-0 items-center gap-1 self-center text-[12px] text-fg-subtle transition-colors hover:text-fg"
				>
					Device <ArrowRight class="size-3" />
				</a>
			{/if}
		</div>

		<div class="grid grid-cols-3 gap-3">
			{#each tiles as t (t.label)}
				<div class="rounded-lg border border-line bg-canvas p-3">
					<div class="text-[12px] text-fg-subtle">{t.label}</div>
					<div class="mt-1 num text-[15px] font-medium text-fg">{t.value}</div>
				</div>
			{/each}
		</div>

		<div>
			<div class="mb-3 flex items-center justify-between gap-3">
				<h3 class="text-[13px] font-medium text-fg">History</h3>
				<Segmented bind:value={range} options={historyRanges} label="Time range" />
			</div>
			<CheckHistory {check} {range} />
		</div>

		<div>
			<h3 class="mb-2 text-[13px] font-medium text-fg">Latest results</h3>
			{#if results.length}
				<ul class="divide-y divide-line rounded-lg border border-line">
					{#each results as r (r.at)}
						<li class="flex items-center gap-3 px-3 py-2 text-[12.5px]">
							<StatusDot status={r.ok ? 'online' : 'offline'} />
							<span class="sr-only">{r.ok ? 'Passed' : 'Failed'}:</span>
							<span class="w-32 shrink-0 num text-fg-subtle">{dateTime(r.at)}</span>
							<span class="min-w-0 flex-1 truncate text-fg-muted" title={r.detail}>{r.detail}</span>
							<span class="num text-fg">{latency(r.latencyMs)}</span>
						</li>
					{/each}
				</ul>
			{:else}
				<p class="rounded-lg border border-line px-3 py-6 text-center text-[13px] text-fg-subtle">
					No results yet.
				</p>
			{/if}
		</div>
	</div>
</Dialog>
