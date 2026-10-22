<!--
	A modal dialog: a sheet from the bottom on phones, centered above that. It
	traps focus, closes on Escape or a click outside, and gives focus back to
	what opened it. Open it with `bind:open`, or give it a `trigger` snippet
	and spread `props` on the element that opens it.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Dialog } from 'bits-ui';
	import X from '@lucide/svelte/icons/x';
	import { iconButtonClass } from './button.js';

	interface Props {
		open?: boolean;
		title: string;
		description?: string;
		trigger?: Snippet<[{ props: Record<string, unknown> }]>;
		children: Snippet;
		/** Buttons along the bottom, such as Cancel and Save. */
		footer?: Snippet;
		/** The widest the dialog gets, as a max-w-* class. */
		width?: string;
	}

	let {
		open = $bindable(false),
		title,
		description,
		trigger,
		children,
		footer,
		width = 'sm:max-w-lg'
	}: Props = $props();
</script>

<Dialog.Root bind:open>
	{#if trigger}
		<Dialog.Trigger>
			{#snippet child({ props })}
				{@render trigger({ props })}
			{/snippet}
		</Dialog.Trigger>
	{/if}
	<Dialog.Portal>
		<Dialog.Overlay class="fixed inset-0 z-50 bg-black/70 fade" />
		<Dialog.Content
			class={[
				'fixed inset-x-0 bottom-0 z-50 flex max-h-[92dvh] pop flex-col overflow-hidden rounded-t-xl border border-line-strong bg-surface shadow-2xl outline-none',
				'sm:inset-0 sm:m-auto sm:h-fit sm:w-[calc(100%-2rem)] sm:rounded-xl',
				width
			]}
		>
			<div class="flex items-start justify-between gap-4 px-5 pt-5 pb-3">
				<div class="min-w-0">
					<Dialog.Title level={2} class="text-[16px] font-semibold tracking-[-0.01em] text-fg">
						{title}
					</Dialog.Title>
					{#if description}
						<Dialog.Description class="mt-1 text-[13px] text-fg-subtle">
							{description}
						</Dialog.Description>
					{/if}
				</div>
				<Dialog.Close class={[iconButtonClass(), '-mt-1 -mr-2']} aria-label="Close">
					<X class="size-4" />
				</Dialog.Close>
			</div>
			<div class="overflow-y-auto px-5 pb-5">{@render children()}</div>
			{#if footer}
				<div class="flex justify-end gap-2 border-t border-line bg-canvas/40 px-5 py-3">
					{@render footer()}
				</div>
			{/if}
		</Dialog.Content>
	</Dialog.Portal>
</Dialog.Root>
