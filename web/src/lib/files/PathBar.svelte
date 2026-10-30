<!--
	The path as clickable breadcrumbs, which turn into a text field when you
	click the empty space, the pencil, or press Ctrl-L.
-->
<script lang="ts">
	import { tick } from 'svelte';
	import ChevronRight from '@lucide/svelte/icons/chevron-right';
	import CornerDownLeft from '@lucide/svelte/icons/corner-down-left';
	import House from '@lucide/svelte/icons/house';
	import Pencil from '@lucide/svelte/icons/pencil';
	import { crumbs } from './paths.js';

	interface Props {
		path: string | null;
		home: string | null;
		ongo: (path: string) => void;
		class?: string;
	}

	let { path, home, ongo, class: className }: Props = $props();

	let editing = $state(false);
	let value = $state('');
	let input = $state<HTMLInputElement>();
	let scroller = $state<HTMLDivElement>();

	async function startEdit() {
		value = path ?? '';
		editing = true;
		await tick();
		input?.select();
	}

	function onKey(e: KeyboardEvent) {
		const typing = (e.target as HTMLElement | null)?.closest(
			'input, textarea, [contenteditable="true"]'
		);
		if (
			(e.ctrlKey || e.metaKey) &&
			e.key.toLowerCase() === 'l' &&
			!typing &&
			!document.querySelector('[role=dialog]')
		) {
			e.preventDefault();
			void startEdit();
		}
	}

	// A long path keeps its end, the folder you're in, in view.
	$effect(() => {
		void [path, editing];
		if (scroller) {
			scroller.scrollLeft = scroller.scrollWidth;
		}
	});

	const all = $derived(path ? crumbs(path) : []);
	/** Inside the home folder, the trail starts at ~ and not at /home/user. */
	const homeIdx = $derived(
		home && path && (path === home || path.startsWith(`${home}/`))
			? all.findIndex((c) => c.path === home)
			: -1
	);
	const shown = $derived(homeIdx >= 0 ? all.slice(homeIdx) : all);

	function submit() {
		const v = value.trim();
		editing = false;
		if (v) {
			ongo(v.replace(/^~(?=$|\/)/, home ?? '~'));
		}
	}
</script>

<svelte:window onkeydown={onKey} />

{#if editing}
	<form
		class={[
			'flex h-8 min-w-0 items-center rounded-md border border-fg-subtle bg-canvas',
			className
		]}
		onsubmit={(e) => {
			e.preventDefault();
			submit();
		}}
	>
		<input
			bind:this={input}
			bind:value
			onkeydown={(e) => {
				if (e.key === 'Escape') {
					e.stopPropagation();
					editing = false;
				}
			}}
			onblur={() => (editing = false)}
			spellcheck="false"
			autocomplete="off"
			aria-label="Path"
			placeholder="/path/to/folder or ~/folder"
			class="h-full min-w-0 flex-1 bg-transparent px-2.5 font-mono text-[12.5px] text-fg placeholder:text-fg-faint focus:outline-none focus-visible:shadow-none"
		/>
		<button
			type="submit"
			onmousedown={(e) => e.preventDefault()}
			class="flex h-full items-center px-2 text-fg-faint hover:text-fg"
			aria-label="Go"
		>
			<CornerDownLeft class="size-3.5" />
		</button>
	</form>
{:else}
	<div
		class={[
			'group flex h-8 min-w-0 items-center rounded-md border border-line-strong bg-canvas transition-colors hover:border-line-heavy',
			className
		]}
	>
		<!-- A click on the empty space opens the text field; the pencil does it for the keyboard. -->
		<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
		<div
			bind:this={scroller}
			class="flex min-w-0 flex-1 [scrollbar-width:none] items-center overflow-x-auto px-1"
			onclick={(e) => e.target === e.currentTarget && startEdit()}
		>
			{#each shown as c, i (c.path)}
				{@const last = i === shown.length - 1}
				{@const isHome = homeIdx >= 0 && i === 0}
				<span class="flex shrink-0 items-center">
					{#if i > 0 && c.path !== '/' && shown[i - 1].path !== '/'}
						<ChevronRight class="size-3 shrink-0 text-fg-faint" />
					{/if}
					<button
						type="button"
						onclick={() => ongo(c.path)}
						title={c.path}
						class={[
							'flex h-6 items-center gap-1 rounded px-1.5 font-mono text-[12.5px] whitespace-nowrap transition-colors',
							last ? 'text-fg' : 'text-fg-subtle hover:bg-hover hover:text-fg'
						]}
					>
						{#if isHome}
							<House class="size-3.5" />
							<span>~</span>
						{:else}
							{c.name}
						{/if}
					</button>
					{#if c.path === '/' && shown.length > 1}
						<ChevronRight class="size-3 shrink-0 text-fg-faint" />
					{/if}
				</span>
			{/each}
			<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
			<span class="h-full min-w-6 flex-1 cursor-text" onclick={startEdit}></span>
		</div>
		<button
			type="button"
			onclick={startEdit}
			class="flex h-full shrink-0 items-center px-2 text-fg-faint transition-colors hover:text-fg"
			aria-label="Edit path"
			title="Type a path (Ctrl-L)"
		>
			<Pencil class="size-3" />
		</button>
	</div>
{/if}
