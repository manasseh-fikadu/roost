<!--
	The top of the container page: its name and state, its compose project, its
	image, short ID and machine, and, for admins, the actions that make sense
	in its state.
-->
<script lang="ts">
	import Box from '@lucide/svelte/icons/box';
	import type { DockerContainerDetail } from '#lib/api/types.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import CopyButton from '#lib/ui/CopyButton.svelte';
	import { actionCopy, actionIcons } from '../actions.js';
	import type { DockerAction } from '../api.js';
	import { actionsFor, shortId } from '../docker.js';
	import StateBadge from '../StateBadge.svelte';
	import { badgeOf } from './helpers.js';

	interface Props {
		container: DockerContainerDetail;
		deviceName: string;
		/** Whether to offer the actions. */
		admin: boolean;
		/** Called with the action an admin picked. The page asks them to confirm. */
		onaction: (action: DockerAction) => void;
	}

	let { container: d, deviceName, admin, onaction }: Props = $props();

	const actions = $derived(actionsFor(d.state.status));
</script>

<div class="mb-6 flex flex-wrap items-start justify-between gap-4">
	<div class="flex min-w-0 items-center gap-4">
		<div
			class="hidden size-12 shrink-0 items-center justify-center rounded-xl border border-line-strong bg-raised text-fg sm:flex"
		>
			<Box class="size-6" strokeWidth={1.6} />
		</div>
		<div class="min-w-0">
			<div class="flex flex-wrap items-center gap-2">
				<h1
					class="min-w-0 text-[24px] leading-8 font-semibold tracking-[-0.02em] break-all text-fg"
				>
					{d.name}
				</h1>
				<StateBadge container={badgeOf(d)} />
				{#if d.project}
					<Badge mono>
						{d.project}{d.service && d.service !== d.project ? ` / ${d.service}` : ''}
					</Badge>
				{/if}
			</div>
			<p class="mt-0.5 flex min-w-0 items-center gap-1 font-mono text-[12px] text-fg-subtle">
				<span class="truncate">{d.image} · {shortId(d.id)} · {deviceName}</span>
				<span class="shrink-0"><CopyButton value={d.id} label="Copy container ID" /></span>
			</p>
		</div>
	</div>
	{#if admin}
		<div class="flex flex-wrap gap-2">
			{#each actions as action (action)}
				{@const Icon = actionIcons[action]}
				<Button
					variant={action === 'start' ? 'primary' : 'secondary'}
					onclick={() => onaction(action)}
				>
					{#snippet icon()}<Icon class="size-3.5" />{/snippet}
					{actionCopy[action].label}
				</Button>
			{/each}
		</div>
	{/if}
</div>
