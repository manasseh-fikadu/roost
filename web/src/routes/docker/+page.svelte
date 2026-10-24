<script lang="ts">
	import ContainerIcon from '@lucide/svelte/icons/container';
	import { resolve } from '$app/paths';
	import { containerStatus, groupContainers } from '#lib/alerts.js';
	import type { Container } from '#lib/api/types.js';
	import { ago, dateTime, plural } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';
	import { statusMeta } from '#lib/status.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import PageHeader from '#lib/ui/PageHeader.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	const snapshot = $derived(live.snapshot);
	const containers = $derived(snapshot?.containers ?? []);
	const groups = $derived(groupContainers(containers));
	const hub = $derived(snapshot?.devices.find((d) => d.self));
	const collector = $derived(snapshot?.collectors.find((c) => c.name === 'Docker'));
	const now = $derived(snapshot ? Date.parse(snapshot.generatedAt) : Date.now());
	const running = $derived(containers.filter((c) => c.state === 'running').length);

	function checkName(id: string): string {
		return snapshot?.checks.find((c) => c.id === id)?.name ?? id;
	}

	function ports(c: Container): string {
		return c.ports
			.map(
				(p) =>
					(p.public ? `${p.public}→${p.private}` : `${p.private}`) +
					(p.type === 'tcp' ? '' : `/${p.type}`)
			)
			.join(', ');
	}

	function since(c: Container): string {
		if (c.state === 'running' && c.startedAt) {
			return `up since ${dateTime(c.startedAt)}`;
		}
		return c.finishedAt ? `stopped ${ago(c.finishedAt, now)}` : c.status;
	}
</script>

<PageHeader
	title="Docker"
	description={snapshot && collector
		? `Containers on ${hub?.name ?? 'the hub'}, by compose project: ${running} of ${plural(containers.length, 'container')} running.`
		: 'Containers on the hub, by compose project.'}
/>

{#if !snapshot}
	<Card><Skeleton class="m-4 h-24" /></Card>
{:else if !collector}
	<Card>
		<EmptyState title="Docker isn't running here" icon={ContainerIcon}>
			Roost finds no Docker socket on the machine it runs on. Other machines' containers come with
			#29.
		</EmptyState>
	</Card>
{:else if collector.state === 'failing' && containers.length === 0}
	<Card>
		<EmptyState title="Roost can't read Docker" icon={ContainerIcon}>
			{collector.error || 'Only admins see why.'}
		</EmptyState>
	</Card>
{:else if containers.length === 0}
	<Card>
		<EmptyState title="No containers" icon={ContainerIcon}>
			Docker runs on {hub?.name ?? 'the hub'}, with no containers yet.
		</EmptyState>
	</Card>
{:else}
	<div class="space-y-4">
		{#each groups as g (g.project ?? '')}
			{@const up = g.containers.filter((c) => c.state === 'running').length}
			<Card
				title={g.project ?? 'Other containers'}
				description={g.project ? 'Docker Compose project' : 'Not in a compose project'}
			>
				{#snippet actions()}
					<span class="num text-[12px] text-fg-subtle">{up}/{g.containers.length} running</span>
				{/snippet}
				<ul class="divide-y divide-line">
					{#each g.containers as c (c.id)}
						{@const status = containerStatus(c)}
						<li class="flex flex-wrap items-start gap-x-4 gap-y-1.5 px-4 py-3">
							<div class="flex min-w-0 flex-1 basis-64 items-start gap-3">
								<StatusDot {status} pulse={status === 'online'} class="mt-1.5" />
								<div class="min-w-0">
									<div class="flex flex-wrap items-center gap-2">
										<span class="truncate text-[14px] font-medium text-fg">{c.name}</span>
										{#if c.health}
											<Badge
												tone={c.health === 'healthy'
													? 'green'
													: c.health === 'unhealthy'
														? 'red'
														: 'gray'}
											>
												{c.health}
											</Badge>
										{/if}
										{#if c.problem}
											<Badge tone={status === 'offline' ? 'red' : 'amber'}>{c.problem}</Badge>
										{/if}
									</div>
									<p class="truncate font-mono text-[11.5px] text-fg-subtle" title={c.image}>
										{c.image}
									</p>
								</div>
							</div>
							<div class="grid min-w-48 grid-cols-[auto_1fr] gap-x-3 gap-y-0.5 text-[12px]">
								<span class="text-fg-faint">state</span>
								<span class={statusMeta[status].text}>{c.state} · {since(c)}</span>
								{#if c.ports.length}
									<span class="text-fg-faint">ports</span>
									<span class="font-mono text-fg-muted">{ports(c)}</span>
								{/if}
								{#if c.restartCount > 0}
									<span class="text-fg-faint">restarts</span>
									<span class="num text-fg-muted">{c.restartCount}</span>
								{/if}
								{#if c.checkIds.length}
									<span class="text-fg-faint">checks</span>
									<span class="flex flex-wrap gap-x-2">
										{#each c.checkIds as id (id)}
											<a
												href="{resolve('/checks')}?check={encodeURIComponent(id)}"
												class="text-fg-muted hover:text-fg hover:underline">{checkName(id)}</a
											>
										{/each}
									</span>
								{/if}
							</div>
						</li>
					{/each}
				</ul>
			</Card>
		{/each}
		<p class="text-[12px] text-fg-faint">
			A container that stops cleanly only gets an alert if its restart policy says it should run.
			Logs, images and controls come with #29.
		</p>
	</div>
{/if}
