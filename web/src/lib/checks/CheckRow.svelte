<!--
	A check in the Checks page's list: its state, its latest results as bars,
	its latest latency and its uptime. Admins get a menu to run, change, pause
	or delete it.
-->
<script lang="ts">
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import Monitor from '@lucide/svelte/icons/monitor';
	import Pause from '@lucide/svelte/icons/pause';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Play from '@lucide/svelte/icons/play';
	import RefreshCw from '@lucide/svelte/icons/refresh-cw';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import { resolve } from '$app/paths';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { putCheck, runCheck } from '#lib/api/client.js';
	import type { Check, CheckResult } from '#lib/api/types.js';
	import UptimeBars from '#lib/charts/UptimeBars.svelte';
	import { checkStatus, inputOf, kindLabels, stateLabels, targetOf } from '#lib/checks.js';
	import { ago, latency, percent } from '#lib/format.js';
	import { statusMeta } from '#lib/status.js';
	import { toast } from '#lib/toast.svelte.js';
	import Badge from '#lib/ui/Badge.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import MenuSeparator from '#lib/ui/MenuSeparator.svelte';
	import StatusDot from '#lib/ui/StatusDot.svelte';

	interface Props {
		check: Check;
		/** The check's latest results, oldest first. */
		recent: CheckResult[];
		/** The hub's time, in Unix milliseconds, for "5m ago". */
		now: number;
		onopen: () => void;
		onedit: () => void;
		ondelete: () => void;
	}

	let { check, recent, now, onopen, onedit, ondelete }: Props = $props();

	let busy = $state(false);

	const last = $derived(check.last);
	const meta = $derived(statusMeta[checkStatus[check.state]]);

	async function act(action: () => Promise<unknown>) {
		busy = true;
		try {
			await action();
		} catch {
			// The client showed the error.
		} finally {
			busy = false;
		}
	}

	async function run() {
		const res = await runCheck(check.id);
		toast(
			`${check.name} ${res.ok ? 'passed' : 'failed'}: ${res.detail}`,
			res.ok ? 'default' : 'error'
		);
	}

	function togglePause() {
		const paused = !check.paused;
		return putCheck(check.id, inputOf(check, { paused }), paused ? 'Paused' : 'Resumed');
	}
</script>

<li
	class="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3 transition-colors hover:bg-hover/40"
>
	<button onclick={onopen} class="flex min-w-0 flex-1 basis-60 items-center gap-3 text-left">
		<StatusDot status={checkStatus[check.state]} pulse={check.state === 'passing'} />
		<span class="min-w-0">
			<span class="flex items-center gap-2">
				<span
					class={['truncate text-[14px] font-medium', check.paused ? 'text-fg-subtle' : 'text-fg']}
				>
					{check.name}
				</span>
				<Badge mono>{kindLabels[check.kind]}</Badge>
				{#if check.paused}
					<Badge>Paused</Badge>
				{/if}
			</span>
			<span class="block truncate font-mono text-[11.5px] text-fg-subtle">{targetOf(check)}</span>
		</span>
	</button>
	<UptimeBars results={recent} class="hidden w-56 sm:flex" />
	<div class="ml-auto flex items-center gap-4">
		<div class="w-24 text-right whitespace-nowrap">
			<div class={['num text-[13px]', check.state === 'passing' ? 'text-fg' : meta.text]}>
				{check.state === 'passing' ? latency(last?.latencyMs) : stateLabels[check.state]}
			</div>
			<div class="truncate num text-[11px] text-fg-faint">
				{last ? ago(last.at, now) : check.stateReason}
			</div>
		</div>
		<div class="w-14 text-right">
			<div class="num text-[13px] text-fg">{percent(check.uptime, 1)}</div>
			<div class="text-[11px] text-fg-faint">24h</div>
		</div>
		{#if check.deviceId}
			<a
				href={resolve('/devices/[id]', { id: check.deviceId })}
				class="inline-flex size-8 shrink-0 items-center justify-center rounded-md text-fg-subtle transition-colors hover:bg-hover hover:text-fg"
				aria-label="Open the device"
				title="Open the device"
			>
				<Monitor class="size-4" />
			</a>
		{:else}
			<AdminOnly>
				<Menu>
					{#snippet trigger({ props })}
						<IconButton {...props} label="Actions for {check.name}" disabled={busy}>
							{#if busy}
								<RefreshCw class="size-4 animate-spin" />
							{:else}
								<Ellipsis class="size-4" />
							{/if}
						</IconButton>
					{/snippet}
					<MenuItem icon={Play} onSelect={() => act(run)}>Run now</MenuItem>
					<MenuItem icon={Pencil} onSelect={onedit}>Edit</MenuItem>
					<MenuItem icon={check.paused ? Play : Pause} onSelect={() => act(togglePause)}>
						{check.paused ? 'Resume' : 'Pause'}
					</MenuItem>
					<MenuSeparator />
					<MenuItem icon={Trash2} danger onSelect={ondelete}>Delete</MenuItem>
				</Menu>
				{#snippet fallback()}
					<span class="size-8 shrink-0"></span>
				{/snippet}
			</AdminOnly>
		{/if}
	</div>
</li>
