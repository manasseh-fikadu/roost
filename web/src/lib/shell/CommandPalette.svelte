<!--
	The command palette: ⌘K, Ctrl+K or / anywhere opens it, typing filters the
	pages and devices, and Enter jumps to the chosen one. Once something is
	typed it also lists actions, such as opening a terminal on a machine or
	restarting a container, which an admin runs from here.
-->
<script lang="ts">
	import { Command, Dialog, computeCommandScore } from 'bits-ui';
	import Bell from '@lucide/svelte/icons/bell';
	import CircleCheck from '@lucide/svelte/icons/circle-check';
	import Container from '@lucide/svelte/icons/container';
	import CornerDownLeft from '@lucide/svelte/icons/corner-down-left';
	import FolderOpen from '@lucide/svelte/icons/folder-open';
	import Keyboard from '@lucide/svelte/icons/keyboard';
	import Play from '@lucide/svelte/icons/play';
	import Plus from '@lucide/svelte/icons/plus';
	import RotateCw from '@lucide/svelte/icons/rotate-cw';
	import Search from '@lucide/svelte/icons/search';
	import Square from '@lucide/svelte/icons/square';
	import SquareTerminal from '@lucide/svelte/icons/square-terminal';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';
	import { ackAlert } from '#lib/api/client.js';
	import { runCheckNow } from '#lib/checks/run.js';
	import { deviceIcon, shortName } from '#lib/device.js';
	import ActionDialog from '#lib/docker/ActionDialog.svelte';
	import type { PendingAction } from '#lib/docker/actions.js';
	import { live } from '#lib/live.svelte.js';
	import { toast } from '#lib/toast.svelte.js';
	import Kbd from '#lib/ui/Kbd.svelte';
	import type { Icon } from '#lib/icon.js';
	import { paletteActions, type PaletteAction } from './actions.js';
	import { navFor } from './nav.js';

	interface Props {
		open?: boolean;
		/** Shows the list of keyboard shortcuts. */
		onshortcuts?: () => void;
	}

	let { open = $bindable(false), onshortcuts }: Props = $props();

	/** What's typed in the search box. */
	let search = $state('');
	$effect(() => {
		if (!open) {
			search = '';
		}
	});

	/** The container an admin chose to start, stop or restart, and where it runs. */
	let pending = $state<PendingAction | null>(null);
	let pendingDevice = $state({ id: '', name: '' });

	interface Entry {
		/** Unique among the entries. */
		id: string;
		label: string;
		hint: string;
		icon: Icon;
		href: string;
		/** What the search matches, besides the label. */
		keywords: string[];
		/** What happens instead of going to `href`, for an action. */
		run?: () => void;
	}

	const actionIcons: Record<PaletteAction['kind'], Icon> = {
		terminal: SquareTerminal,
		files: FolderOpen,
		docker: Container,
		container: Container,
		'run-check': Play,
		'ack-alert': Bell,
		'new-check': Plus,
		shortcuts: Keyboard
	};
	const containerIcons = { start: Play, stop: Square, restart: RotateCw };

	function actionEntry(a: PaletteAction): Entry {
		const base = {
			id: `action:${a.id}`,
			label: a.label,
			hint: a.hint,
			keywords: [a.label, ...a.keywords]
		};
		const device = (path: '/terminal' | '/files' | '/docker', id: string) =>
			`${resolve(path)}?device=${encodeURIComponent(id)}`;
		switch (a.kind) {
			case 'terminal':
				return { ...base, icon: actionIcons.terminal, href: device('/terminal', a.deviceId) };
			case 'files':
				return { ...base, icon: actionIcons.files, href: device('/files', a.deviceId) };
			case 'docker':
				return { ...base, icon: actionIcons.docker, href: device('/docker', a.deviceId) };
			case 'container':
				return {
					...base,
					icon: containerIcons[a.action],
					href: '',
					run: () => {
						pendingDevice = { id: a.deviceId, name: a.deviceName };
						pending = {
							id: a.containerId,
							name: a.containerName,
							state: '',
							action: a.action
						};
					}
				};
			case 'run-check':
				return {
					...base,
					icon: actionIcons['run-check'],
					href: '',
					run: () => {
						const check = live.snapshot?.checks.find((c) => c.id === a.checkId);
						if (check) {
							void runCheckNow(check).catch(() => {});
						}
					}
				};
			case 'ack-alert':
				return { ...base, icon: CircleCheck, href: '', run: () => void acknowledge(a.alertId) };
			case 'new-check':
				return { ...base, icon: actionIcons['new-check'], href: `${resolve('/checks')}?new` };
			case 'shortcuts':
				return { ...base, icon: actionIcons.shortcuts, href: '', run: () => onshortcuts?.() };
		}
	}

	async function acknowledge(id: number) {
		try {
			await ackAlert(id, true);
		} catch {
			return;
		}
		toast('Acknowledged', 'default', {
			duration: 6000,
			action: { label: 'Undo', run: () => void ackAlert(id, false).catch(() => {}) }
		});
	}

	const groups = $derived.by(() => {
		const pages: Entry[] = navFor(isAdmin(page.data.me)).flatMap((s) =>
			s.items.map((item) => ({
				id: `page:${item.href}`,
				label: item.label,
				hint: 'Page',
				icon: item.icon,
				href: resolve(item.href),
				keywords: [item.label]
			}))
		);
		const devices: Entry[] = (live.snapshot?.devices ?? []).map((d) => ({
			id: `device:${d.id}`,
			label: d.name,
			hint: [shortName(d), d.ips[0]].filter(Boolean).join(' · '),
			icon: deviceIcon(d),
			href: resolve('/devices/[id]', { id: d.id }),
			keywords: [d.name, d.dnsName, ...d.ips]
		}));
		// There can be dozens of actions, so they wait until something is typed.
		const actions = search.trim()
			? paletteActions(live.snapshot, isAdmin(page.data.me)).map(actionEntry)
			: [];
		return [
			{ heading: 'Pages', entries: pages },
			{ heading: 'Devices', entries: devices },
			{ heading: 'Actions', entries: actions }
		].filter((g) => g.entries.length > 0);
	});

	// Match on what people see and type, not on the entries' ids.
	function filter(_id: string, search: string, keywords?: string[]): number {
		return computeCommandScore(keywords?.join(' ') ?? '', search);
	}

	function choose(entry: Entry) {
		open = false;
		if (entry.run) {
			entry.run();
		} else {
			goto(entry.href);
		}
	}

	function onkeydown(e: KeyboardEvent) {
		if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
			e.preventDefault();
			open = !open;
		}
	}
</script>

<svelte:window {onkeydown} />

<Dialog.Root bind:open>
	<Dialog.Portal>
		<Dialog.Overlay class="fixed inset-0 z-50 bg-black/70 fade" />
		<Dialog.Content
			class="fixed inset-x-4 top-[12vh] z-50 mx-auto max-w-xl pop overflow-hidden rounded-xl border border-line-strong bg-surface shadow-2xl outline-none"
		>
			<Dialog.Title class="sr-only">Search pages, devices and actions</Dialog.Title>
			<Command.Root label="Search pages, devices and actions" {filter} loop>
				<div class="flex items-center gap-2 border-b border-line px-4">
					<Search class="size-4 shrink-0 text-fg-faint" />
					<Command.Input
						bind:value={search}
						placeholder="Search pages, devices and actions…"
						class="h-12 min-w-0 flex-1 bg-transparent text-[15px] text-fg placeholder:text-fg-faint focus:outline-none focus-visible:shadow-none"
					/>
					<Kbd>esc</Kbd>
				</div>
				<Command.List class="max-h-[50vh] overflow-y-auto p-1.5">
					<Command.Viewport>
						<Command.Empty class="px-3 py-8 text-center text-[13px] text-fg-subtle">
							Nothing matches.
						</Command.Empty>
						{#each groups as group (group.heading)}
							<Command.Group value={group.heading}>
								<Command.GroupHeading
									class="px-2.5 pt-2 pb-1 font-mono text-[10.5px] tracking-wider text-fg-faint uppercase"
								>
									{group.heading}
								</Command.GroupHeading>
								<Command.GroupItems>
									{#each group.entries as entry (entry.id)}
										<Command.Item
											value={entry.id}
											keywords={entry.keywords}
											onSelect={() => choose(entry)}
											class="group flex h-10 cursor-pointer items-center gap-3 rounded-md px-2.5 outline-none data-selected:bg-hover"
										>
											<entry.icon class="size-4 shrink-0 text-fg-subtle" />
											<span class="truncate text-[14px] text-fg">{entry.label}</span>
											<span class="truncate font-mono text-[11px] text-fg-faint">{entry.hint}</span>
											<CornerDownLeft
												class="ml-auto hidden size-3.5 shrink-0 text-fg-faint group-data-selected:block"
											/>
										</Command.Item>
									{/each}
								</Command.GroupItems>
							</Command.Group>
						{/each}
					</Command.Viewport>
				</Command.List>
				{#if !search.trim()}
					<p class="border-t border-line px-4 py-2 text-[12px] text-fg-faint">
						Type to find an action, such as “restart” or “terminal”.
					</p>
				{/if}
			</Command.Root>
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>

<ActionDialog deviceId={pendingDevice.id} deviceName={pendingDevice.name} bind:pending />
