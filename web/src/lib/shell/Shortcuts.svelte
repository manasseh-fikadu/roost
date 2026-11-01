<!--
	The keyboard shortcuts that work on every page: `?` opens the list of
	them, and `g` then a number goes to the page with that number in the
	sidebar. Nothing happens while someone types in a field, the terminal or an
	editor, or while a dialog or menu is open.
-->
<script lang="ts">
	import { goto } from '$app/navigation';
	import { resolve } from '$app/paths';
	import { page } from '$app/state';
	import { isAdmin } from '#lib/access.js';
	import { isTyping, shortcutGroups, stepChord } from '#lib/shortcuts.js';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Kbd from '#lib/ui/Kbd.svelte';
	import { pagesFor } from './nav.js';

	interface Props {
		open?: boolean;
		/** What this computer calls the command key: ⌘ or Ctrl. */
		mod: string;
	}

	let { open = $bindable(false), mod }: Props = $props();

	const groups = $derived(shortcutGroups(mod));
	const pages = $derived(pagesFor(isAdmin(page.data.me)));

	/** When `g` was pressed, while the page number can still come. */
	let armedAt: number | null = null;

	function onkeydown(e: KeyboardEvent) {
		if (e.defaultPrevented || e.metaKey || e.ctrlKey || e.altKey) {
			return;
		}
		if (
			isTyping(e.target as Element | null) ||
			document.querySelector('[role=dialog],[role=menu]')
		) {
			armedAt = null;
			return;
		}
		if (e.key === '?') {
			e.preventDefault();
			open = true;
			return;
		}
		const step = stepChord(armedAt, e.key, Date.now());
		armedAt = step.armedAt;
		const target = step.page ? pages[step.page - 1] : undefined;
		if (target) {
			e.preventDefault();
			void goto(resolve(target.href));
		}
	}
</script>

<svelte:window {onkeydown} />

<Dialog bind:open title="Keyboard shortcuts" width="sm:max-w-2xl">
	<div class="grid gap-x-8 gap-y-5 sm:grid-cols-2">
		{#each groups as group (group.heading)}
			<section>
				<h3 class="pb-1.5 font-mono text-[10.5px] tracking-wider text-fg-faint uppercase">
					{group.heading}
				</h3>
				<ul class="divide-y divide-line">
					{#each group.shortcuts as s (s.label)}
						<li class="flex items-center justify-between gap-3 py-2 text-[13px]">
							<span class="text-fg-muted">{s.label}</span>
							<span class="flex shrink-0 items-center gap-1">
								{#each s.keys as key, i (i)}
									{#if i > 0 && s.sequence}
										<span class="text-[11px] text-fg-faint">then</span>
									{/if}
									<Kbd>{key}</Kbd>
								{/each}
							</span>
						</li>
					{/each}
				</ul>
			</section>
		{/each}
		<section class="sm:col-span-2">
			<h3 class="pb-1.5 font-mono text-[10.5px] tracking-wider text-fg-faint uppercase">
				Pages, for <Kbd>g</Kbd> then the number
			</h3>
			<ul class="grid gap-x-8 sm:grid-cols-2">
				{#each pages as p, i (p.href)}
					<li class="flex items-center justify-between gap-3 py-1.5 text-[13px]">
						<span class="text-fg-muted">{p.label}</span>
						<Kbd>{i + 1}</Kbd>
					</li>
				{/each}
			</ul>
		</section>
	</div>
</Dialog>
