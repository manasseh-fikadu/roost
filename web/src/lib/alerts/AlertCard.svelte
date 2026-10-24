<!--
	An alert: what's wrong, since when, and on which device. Admins can
	acknowledge it, snooze it and write notes on it. Compact, it leaves out
	its badge and cuts its detail short, for cards on Overview and devices.
-->
<script lang="ts">
	import Check from '@lucide/svelte/icons/check';
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import CircleAlert from '@lucide/svelte/icons/circle-alert';
	import Info from '@lucide/svelte/icons/info';
	import MessageSquare from '@lucide/svelte/icons/message-square';
	import MoonStar from '@lucide/svelte/icons/moon-star';
	import OctagonAlert from '@lucide/svelte/icons/octagon-alert';
	import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
	import { resolve } from '$app/paths';
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { severityMeta, snoozeOptions } from '#lib/alerts.js';
	import { ackAlert, snoozeAlert } from '#lib/api/client.js';
	import type { Alert, AlertSeverity, Device } from '#lib/api/types.js';
	import { ago, dateTime } from '#lib/format.js';
	import type { Icon } from '#lib/icon.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import MenuSeparator from '#lib/ui/MenuSeparator.svelte';
	import NotesDialog from './NotesDialog.svelte';

	interface Props {
		alert: Alert;
		/** The device it's on, if it's on one. */
		device?: Device;
		/** The hub's time, in Unix milliseconds, for "5m ago". */
		now: number;
		compact?: boolean;
	}

	let { alert, device, now, compact = false }: Props = $props();

	const icons: Record<AlertSeverity, Icon> = {
		critical: OctagonAlert,
		warning: CircleAlert,
		info: Info
	};

	let busy = $state(false);
	let notesOpen = $state(false);

	const meta = $derived(severityMeta[alert.severity]);
	const resolved = $derived(alert.state === 'resolved');
	const SeverityIcon = $derived(icons[alert.severity]);

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
</script>

<div class={['rounded-lg border p-3', resolved ? 'border-line bg-surface' : meta.box]}>
	<div class="flex items-start gap-2.5">
		<SeverityIcon
			class={['mt-0.5 size-4 shrink-0', resolved ? 'text-fg-faint' : meta.text]}
			strokeWidth={1.8}
		/>
		<div class="min-w-0 flex-1">
			<div class="flex items-start justify-between gap-2">
				<p class={['text-[13px] leading-5 font-medium', resolved ? 'text-fg-muted' : 'text-fg']}>
					<span class="sr-only">{meta.label}:</span>
					{alert.title}
				</p>
				{#if !compact}
					<Badge tone={resolved ? 'gray' : meta.tone} class="mt-px">
						{resolved ? 'Resolved' : meta.label}
					</Badge>
				{/if}
			</div>
			{#if alert.detail}
				<p
					class={[
						'mt-0.5 text-[12.5px] leading-5 break-words text-fg-subtle',
						compact && 'line-clamp-2'
					]}
				>
					{alert.detail}
				</p>
			{/if}
			<div
				class="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 font-mono text-[11px] text-fg-faint"
			>
				<span title={dateTime(alert.openedAt)}>Opened {ago(alert.openedAt, now)}</span>
				{#if alert.resolvedAt}
					<span title={dateTime(alert.resolvedAt)}>· Resolved {ago(alert.resolvedAt, now)}</span>
				{/if}
				{#if device}
					<span>·</span>
					<a
						href={resolve('/devices/[id]', { id: device.id })}
						class="text-fg-subtle hover:text-fg hover:underline"
					>
						{device.name}
					</a>
				{/if}
				{#if alert.checkId}
					<span>·</span>
					<a
						href="{resolve('/checks')}?check={encodeURIComponent(alert.checkId)}"
						class="text-fg-subtle hover:text-fg hover:underline"
					>
						Check
					</a>
				{/if}
			</div>
			{#if alert.ackedAt && !resolved}
				<p class="mt-1.5 flex items-center gap-1 font-mono text-[11px] text-fg-subtle">
					<Check class="size-3" />
					Acknowledged{alert.ackedBy ? ` by ${alert.ackedBy}` : ''} · {ago(alert.ackedAt, now)}
				</p>
			{/if}
			{#if alert.state === 'snoozed' && alert.snoozedUntil}
				<p class="mt-1.5 flex items-center gap-1 font-mono text-[11px] text-fg-subtle">
					<MoonStar class="size-3" />
					Snoozed until {dateTime(alert.snoozedUntil)}{alert.snoozedBy
						? ` by ${alert.snoozedBy}`
						: ''}
				</p>
			{/if}
			<div class="mt-2.5 flex flex-wrap items-center gap-1.5">
				{#if !resolved}
					<AdminOnly>
						{#if alert.ackedAt}
							{#snippet undo()}<RotateCcw class="size-3" />{/snippet}
							<Button
								size="xs"
								icon={undo}
								disabled={busy}
								onclick={() => act(() => ackAlert(alert.id, false))}
							>
								Unacknowledge
							</Button>
						{:else}
							{#snippet tick()}<Check class="size-3" />{/snippet}
							<Button
								size="xs"
								icon={tick}
								disabled={busy}
								onclick={() => act(() => ackAlert(alert.id, true))}
							>
								Acknowledge
							</Button>
						{/if}
						<Menu align="start">
							{#snippet trigger({ props })}
								<Button {...props} size="xs" disabled={busy}>
									{alert.state === 'snoozed' ? 'Snoozed' : 'Snooze'}
									<ChevronDown class="size-3 text-fg-faint" />
								</Button>
							{/snippet}
							{#each snoozeOptions as o (o.minutes)}
								<MenuItem onSelect={() => act(() => snoozeAlert(alert.id, o.minutes))}>
									For {o.label}
								</MenuItem>
							{/each}
							{#if alert.state === 'snoozed'}
								<MenuSeparator />
								<MenuItem onSelect={() => act(() => snoozeAlert(alert.id, null))}>Wake it</MenuItem>
							{/if}
						</Menu>
					</AdminOnly>
				{/if}
				{#snippet bubble()}<MessageSquare class="size-3" />{/snippet}
				<Button size="xs" variant="ghost" icon={bubble} onclick={() => (notesOpen = true)}>
					Notes{alert.notes.length ? ` (${alert.notes.length})` : ''}
				</Button>
			</div>
		</div>
	</div>
</div>
<NotesDialog {alert} bind:open={notesOpen} />
