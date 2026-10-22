<!--
	The command palette: ⌘K or Ctrl+K anywhere opens it, typing filters the
	pages and devices, and Enter jumps to the chosen one.
-->
<script lang="ts">
	import { Command, Dialog, computeCommandScore } from 'bits-ui';
	import CornerDownLeft from '@lucide/svelte/icons/corner-down-left';
	import Search from '@lucide/svelte/icons/search';
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';
	import { deviceIcon, shortName } from '#lib/device.js';
	import { live } from '#lib/live.svelte.js';
	import Kbd from '#lib/ui/Kbd.svelte';
	import type { Icon } from '#lib/icon.js';
	import { navFor } from './nav.js';

	interface Props {
		open?: boolean;
	}

	let { open = $bindable(false) }: Props = $props();

	interface Entry {
		/** Unique among the entries. */
		id: string;
		label: string;
		hint: string;
		icon: Icon;
		href: string;
		/** What the search matches, besides the label. */
		keywords: string[];
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
		return [
			{ heading: 'Pages', entries: pages },
			{ heading: 'Devices', entries: devices }
		].filter((g) => g.entries.length > 0);
	});

	// Match on what people see and type, not on the entries' ids.
	function filter(_id: string, search: string, keywords?: string[]): number {
		return computeCommandScore(keywords?.join(' ') ?? '', search);
	}

	function go(href: string) {
		open = false;
		goto(href);
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
			<Dialog.Title class="sr-only">Jump to a page or device</Dialog.Title>
			<Command.Root label="Jump to a page or device" {filter} loop>
				<div class="flex items-center gap-2 border-b border-line px-4">
					<Search class="size-4 shrink-0 text-fg-faint" />
					<Command.Input
						placeholder="Jump to a page or device…"
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
											onSelect={() => go(entry.href)}
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
			</Command.Root>
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
