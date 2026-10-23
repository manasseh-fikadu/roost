<!--
	A status page's bars: one for each of a check's latest results, oldest on
	the left, green if it passed and red if it failed. Pointing at a bar says
	when it ran and what happened.
-->
<script lang="ts">
	import type { CheckResult } from '#lib/api/types.js';
	import { dateTime, latency } from '#lib/format.js';

	interface Props {
		/** The results, oldest first. Only the latest `count` show. */
		results: CheckResult[];
		/** How many bars there are. Missing results are blank. */
		count?: number;
		class?: string;
	}

	let { results, count = 30, class: className }: Props = $props();

	const shown = $derived(results.slice(-count));
	const blank = $derived(Math.max(0, count - shown.length));
	const passed = $derived(shown.filter((r) => r.ok).length);

	function title(r: CheckResult): string {
		const parts = [dateTime(r.at), r.ok ? 'Passed' : 'Failed', r.detail];
		if (r.latencyMs !== undefined) {
			parts.push(latency(r.latencyMs));
		}
		return parts.filter(Boolean).join(' · ');
	}
</script>

<div
	class={['flex h-5 items-stretch gap-[2px]', className]}
	role="img"
	aria-label={shown.length
		? `${passed} of the latest ${shown.length} results passed`
		: 'No results yet'}
>
	{#each { length: blank }, i (i)}
		<span class="flex-1 rounded-[1px] bg-line"></span>
	{/each}
	{#each shown as r (r.at)}
		<span title={title(r)} class={['flex-1 rounded-[1px]', r.ok ? 'bg-green-fg/80' : 'bg-red-fg']}
		></span>
	{/each}
</div>
