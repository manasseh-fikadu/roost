<!--
	A container's health check: what it runs and how often (the command is for
	admins), and its latest results, newest first.
-->
<script lang="ts">
	import HeartPulse from '@lucide/svelte/icons/heart-pulse';
	import type { DockerContainerDetail } from '#lib/api/types.js';
	import { ago, plural } from '#lib/format.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Card from '#lib/ui/Card.svelte';
	import CopyButton from '#lib/ui/CopyButton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';
	import {
		checkDuration,
		healthCommand,
		healthFacts,
		knownTime,
		millis,
		stripAnsi
	} from './helpers.js';

	interface Props {
		container: DockerContainerDetail;
		/** The time to measure "5m ago" from, in Unix milliseconds. */
		now: number;
	}

	let { container: d, now }: Props = $props();

	const check = $derived(d.healthcheck);
	const health = $derived(d.health);
	const command = $derived(healthCommand(check?.test));
	const facts = $derived(healthFacts(check));
	const results = $derived((health?.log ?? []).toReversed());
	const tone = $derived(
		health?.status === 'healthy' ? 'green' : health?.status === 'unhealthy' ? 'red' : 'amber'
	);
</script>

{#if check || health}
	<Card>
		<header class="flex items-center justify-between gap-3 border-b border-line px-4 py-3">
			<h2 class="flex items-center gap-2 text-[14px] leading-5 font-medium text-fg">
				<HeartPulse class="size-3.5 text-fg-subtle" /> Health check
			</h2>
			{#if health}
				<div class="flex items-center gap-2">
					{#if health.failingStreak > 0}
						<span class="num text-[12px] text-red-fg">
							{plural(health.failingStreak, 'failure')} in a row
						</span>
					{/if}
					<Badge {tone} mono>{health.status.toUpperCase()}</Badge>
				</div>
			{/if}
		</header>
		{#if command || facts.length > 0}
			<div class="border-b border-line px-4 py-3">
				{#if command}
					<div class="flex items-start gap-1">
						<code class="min-w-0 flex-1 font-mono text-[12.5px] break-all text-fg">{command}</code>
						<span class="-my-1 -mr-2 shrink-0"
							><CopyButton value={command} label="Copy command" /></span
						>
					</div>
				{/if}
				{#if facts.length > 0}
					<div class={['text-[12px] text-fg-subtle', command && 'mt-1']}>{facts.join(' · ')}</div>
				{/if}
			</div>
		{/if}
		{#if results.length === 0}
			<p class="px-4 py-3 text-[13px] text-fg-subtle">
				{d.state.running ? 'No results yet.' : 'Health checks run while the container is up.'}
			</p>
		{:else}
			<ul>
				{#each results as r, i (i)}
					{@const took = millis(checkDuration(r))}
					{@const output = stripAnsi(r.output).trim()}
					<li class="flex gap-3 border-b border-line px-4 py-2.5 last:border-0">
						<StatusDot status={r.exitCode === 0 ? 'online' : 'offline'} class="mt-1.5" />
						<div class="min-w-0 flex-1">
							<div class="flex flex-wrap items-baseline gap-x-3 text-[12px]">
								<span class="num text-fg">{knownTime(r.start) ? ago(r.start, now) : '—'}</span>
								<span class={['num', r.exitCode === 0 ? 'text-fg-subtle' : 'text-red-fg']}>
									exit {r.exitCode}
								</span>
								{#if took}
									<span class="num text-fg-faint">{took}</span>
								{/if}
							</div>
							{#if output}
								<pre
									class="mt-1 max-h-24 overflow-auto font-mono text-[11.5px] leading-[1.5] wrap-anywhere whitespace-pre-wrap text-fg-muted">{output}</pre>
							{/if}
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</Card>
{/if}
