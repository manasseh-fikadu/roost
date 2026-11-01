<!--
	The places Roost sends notifications to: each with where it goes, which
	alerts it hears about, and how the last message went. Admins add, change,
	test and delete them.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import Bell from '@lucide/svelte/icons/bell';
	import Ellipsis from '@lucide/svelte/icons/ellipsis';
	import Pencil from '@lucide/svelte/icons/pencil';
	import Plus from '@lucide/svelte/icons/plus';
	import Send from '@lucide/svelte/icons/send';
	import Trash2 from '@lucide/svelte/icons/trash-2';
	import { resolve } from '$app/paths';
	import { deleteChannel, getChannels, putChannel, testChannel } from '#lib/api/client.js';
	import type { NotificationChannel } from '#lib/api/types.js';
	import { ago } from '#lib/format.js';
	import { draftOf, eventNouns, inputOf, kindLabels, routingText } from '#lib/notifications.js';
	import { toast } from '#lib/toast.svelte.js';
	import { deleteWithUndo, isHidden } from '#lib/undo.svelte.js';
	import Badge from '#lib/ui/Badge.svelte';
	import Button from '#lib/ui/Button.svelte';
	import Card from '#lib/ui/Card.svelte';
	import EmptyState from '#lib/ui/EmptyState.svelte';
	import IconButton from '#lib/ui/IconButton.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import MenuSeparator from '#lib/ui/MenuSeparator.svelte';
	import Skeleton from '#lib/ui/Skeleton.svelte';
	import Switch from '#lib/ui/Switch.svelte';
	import ErrorState from '#lib/ui/ErrorState.svelte';
	import ChannelDialog from './ChannelDialog.svelte';
	import ChannelIcon from './ChannelIcon.svelte';

	interface Props {
		/** Whether the tab is showing. The list loads again each time it does, to show how the latest messages went. */
		active: boolean;
	}

	let { active }: Props = $props();

	let channels = $state.raw<NotificationChannel[] | null>(null);
	let failure = $state('');
	/** The IDs of the channels that are doing something. */
	let busy = $state.raw<number[]>([]);

	/** Counts the loads, so only the latest one counts. */
	let latest = 0;

	async function load() {
		const mine = ++latest;
		try {
			const list = await getChannels();
			if (mine === latest) {
				channels = list.channels;
				failure = '';
			}
		} catch (err) {
			if (mine === latest) {
				failure = err instanceof Error ? err.message : String(err);
			}
		}
	}

	$effect(() => {
		if (active) {
			untrack(() => void load());
		}
	});

	function replace(c: NotificationChannel) {
		channels = (channels ?? []).map((x) => (x.id === c.id ? c : x));
	}

	function keep(c: NotificationChannel) {
		const rest = (channels ?? []).filter((x) => x.id !== c.id);
		channels = [...rest, c].sort((a, b) => a.name.localeCompare(b.name));
	}

	async function act(c: NotificationChannel, action: () => Promise<unknown>) {
		busy = [...busy, c.id];
		try {
			await action();
		} catch {
			// The client showed the error.
		} finally {
			busy = busy.filter((id) => id !== c.id);
		}
	}

	async function test(c: NotificationChannel) {
		const res = await testChannel(c.id);
		replace({ ...c, last: res });
		toast(
			res.ok ? `Sent a test to ${c.name}` : `The test to ${c.name} failed: ${res.error}`,
			res.ok ? 'default' : 'error'
		);
	}

	/** Pausing and resuming show at once, and go back if Roost can't save them. */
	async function toggle(c: NotificationChannel, enabled: boolean) {
		replace({ ...c, enabled });
		try {
			replace(
				await putChannel(c.id, inputOf({ ...draftOf(c), enabled }), enabled ? 'Sending' : 'Paused')
			);
		} catch (err) {
			replace(c);
			throw err;
		}
	}

	/** The channel being added (null) or changed, while the dialog is open. */
	let editing = $state<NotificationChannel | null>(null);
	let editOpen = $state(false);

	function edit(c: NotificationChannel | null) {
		editing = c;
		editOpen = true;
	}

	/** The channels a person can see: one that was just deleted is gone while it can be undone. */
	const visible = $derived(channels?.filter((c) => !isHidden(`channel:${c.id}`)) ?? null);

	function remove(c: NotificationChannel) {
		deleteWithUndo({
			key: `channel:${c.id}`,
			message: `Deleted ${c.name}`,
			commit: async () => {
				await deleteChannel(c.id);
				channels = (channels ?? []).filter((x) => x.id !== c.id);
			}
		});
	}
</script>

{#snippet plus()}
	<Plus class="size-3.5" />
{/snippet}

<Card
	title="Channels"
	description="Where Roost sends a message when an alert opens. Each channel picks the alerts it hears about."
>
	{#snippet actions()}
		<Button variant="primary" icon={plus} onclick={() => edit(null)}>Add channel</Button>
	{/snippet}
	{#if channels === null && failure === ''}
		<div class="space-y-2 p-4" aria-busy="true">
			<Skeleton class="h-14" />
			<Skeleton class="h-14" />
		</div>
	{:else if channels === null}
		<ErrorState title="Couldn't load the channels" message={failure} onretry={load} />
	{:else if visible && visible.length === 0}
		<EmptyState title="No channels yet" icon={Bell}>
			Alerts only show up in Roost until you add a place to send them: your phone, a chat room or an
			inbox.
			<div class="mt-3">
				<Button variant="primary" icon={plus} onclick={() => edit(null)}>Add channel</Button>
			</div>
		</EmptyState>
	{:else}
		<ul class="divide-y divide-line">
			{#each visible ?? [] as c (c.id)}
				<li class="flex flex-wrap items-center gap-x-4 gap-y-2 px-4 py-3">
					<div class="flex min-w-0 flex-1 basis-64 items-start gap-3">
						<span
							class={[
								'mt-0.5 flex size-8 shrink-0 items-center justify-center rounded-lg border border-line bg-raised',
								c.enabled ? 'text-fg-muted' : 'text-fg-faint'
							]}
						>
							<ChannelIcon kind={c.kind} class="size-4" />
						</span>
						<div class="min-w-0">
							<div class="flex items-center gap-2">
								<span
									class={[
										'truncate text-[14px] font-medium',
										c.enabled ? 'text-fg' : 'text-fg-subtle'
									]}
								>
									{c.name}
								</span>
								<Badge mono>{kindLabels[c.kind]}</Badge>
								{#if !c.enabled}
									<Badge>Paused</Badge>
								{/if}
							</div>
							<p class="truncate text-[12.5px] text-fg-subtle">{c.summary}</p>
							<p class="text-[12.5px] text-fg-subtle">{routingText(c)}</p>
							{#if c.last}
								<p
									class={['mt-0.5 text-[12px]', c.last.ok ? 'text-fg-faint' : 'text-red-fg']}
									title={c.last.error}
								>
									{#if c.last.ok}
										Sent {eventNouns[c.last.event] ?? 'a message'}
										{ago(c.last.at)}
									{:else}
										Couldn't send {eventNouns[c.last.event] ?? 'a message'}
										{ago(c.last.at)}: {c.last.error}
									{/if}
								</p>
							{/if}
						</div>
					</div>
					<div class="ml-auto flex items-center gap-2">
						<Button
							size="xs"
							loading={busy.includes(c.id)}
							onclick={() => act(c, () => test(c))}
							aria-label="Send a test to {c.name}"
						>
							Send test
						</Button>
						<Switch
							label={c.enabled ? `Pause ${c.name}` : `Resume ${c.name}`}
							bind:checked={() => c.enabled, (on) => void act(c, () => toggle(c, on))}
							disabled={busy.includes(c.id)}
						/>
						<Menu>
							{#snippet trigger({ props })}
								<IconButton {...props} label="Actions for {c.name}">
									<Ellipsis class="size-4" />
								</IconButton>
							{/snippet}
							<MenuItem icon={Pencil} onSelect={() => edit(c)}>Edit</MenuItem>
							<MenuItem icon={Send} onSelect={() => act(c, () => test(c))}>Send test</MenuItem>
							<MenuSeparator />
							<MenuItem icon={Trash2} danger onSelect={() => remove(c)}>Delete</MenuItem>
						</Menu>
					</div>
				</li>
			{/each}
		</ul>
		<p class="border-t border-line px-4 py-2.5 text-[12.5px] text-fg-subtle">
			What Roost sent, and what failed, is in the
			<a
				href="{resolve('/activity')}?kind=notification"
				class="text-fg-muted underline hover:text-fg"
			>
				activity log</a
			>.
		</p>
	{/if}
</Card>

<ChannelDialog channel={editing} bind:open={editOpen} onsaved={keep} />
