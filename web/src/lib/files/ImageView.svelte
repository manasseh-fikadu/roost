<!-- An image, fitted to the window or at its real size, with the checkerboard behind it. -->
<script lang="ts">
	import AlertTriangle from '@lucide/svelte/icons/triangle-alert';
	import Maximize2 from '@lucide/svelte/icons/maximize-2';
	import Minimize2 from '@lucide/svelte/icons/minimize-2';
	import PreviewMessage from './PreviewMessage.svelte';

	let { src, name }: { src: string; name: string } = $props();

	let actual = $state(false);
	let size = $state<{ w: number; h: number } | null>(null);
	let failed = $state(false);

	$effect(() => {
		void src;
		actual = false;
		size = null;
		failed = false;
	});
</script>

{#if failed}
	<PreviewMessage icon={AlertTriangle} title="This image couldn’t be displayed" tone="error">
		The browser can’t decode it, or it can’t be read.
	</PreviewMessage>
{:else}
	<div class="relative size-full">
		<!-- A click toggles the size; the buttons below do it for the keyboard. -->
		<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
		<div
			class={[
				'size-full overflow-auto',
				actual ? 'cursor-zoom-out' : 'flex cursor-zoom-in items-center justify-center p-4 sm:p-8'
			]}
			onclick={() => (actual = !actual)}
		>
			<img
				{src}
				alt={name}
				draggable="false"
				onload={(e) => {
					const img = e.currentTarget as HTMLImageElement;
					size = { w: img.naturalWidth, h: img.naturalHeight };
				}}
				onerror={() => (failed = true)}
				class={[
					'bg-[conic-gradient(#1a1a1a_90deg,#111_90deg_180deg,#1a1a1a_180deg_270deg,#111_270deg)] bg-size-[16px_16px] shadow-[0_0_0_1px_#2e2e2e]',
					actual ? 'max-w-none' : 'max-h-full max-w-full object-contain'
				]}
			/>
		</div>
		<div
			class="absolute bottom-3 left-1/2 flex -translate-x-1/2 items-center gap-1 rounded-lg border border-line-strong bg-surface/90 p-1 backdrop-blur"
		>
			<button
				type="button"
				onclick={() => (actual = false)}
				class={[
					'flex h-7 items-center gap-1.5 rounded-md px-2 text-[12px]',
					!actual ? 'bg-hover text-fg' : 'text-fg-subtle hover:text-fg'
				]}
			>
				<Minimize2 class="size-3" /> Fit
			</button>
			<button
				type="button"
				onclick={() => (actual = true)}
				class={[
					'flex h-7 items-center gap-1.5 rounded-md px-2 text-[12px]',
					actual ? 'bg-hover text-fg' : 'text-fg-subtle hover:text-fg'
				]}
			>
				<Maximize2 class="size-3" /> 100%
			</button>
			{#if size}<span class="px-2 num text-[11px] text-fg-subtle">{size.w}×{size.h}</span>{/if}
		</div>
	</div>
{/if}
