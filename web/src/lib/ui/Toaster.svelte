<!-- Shows the toasts from #lib/toast.svelte.ts. The shell has one. -->
<script lang="ts">
	import { flip } from 'svelte/animate';
	import { fly } from 'svelte/transition';
	import { prefersReducedMotion } from 'svelte/motion';
	import X from '@lucide/svelte/icons/x';
	import { dismiss, toasts } from '#lib/toast.svelte.js';

	const duration = $derived(prefersReducedMotion.current ? 0 : 160);
</script>

<div
	class="pointer-events-none fixed right-4 bottom-4 z-[60] flex w-80 max-w-[calc(100vw-2rem)] flex-col gap-2"
	aria-live="polite"
>
	{#each toasts as t (t.id)}
		<div
			animate:flip={{ duration }}
			transition:fly={{ y: 8, duration }}
			role={t.tone === 'error' ? 'alert' : 'status'}
			class={[
				'pointer-events-auto flex items-start gap-2 rounded-lg border bg-surface px-3.5 py-3 text-[13px] shadow-popover',
				t.tone === 'error' ? 'border-red/40 text-red-fg' : 'border-line-strong text-fg'
			]}
		>
			<span class="min-w-0 flex-1 break-words">{t.message}</span>
			{#if t.action}
				{@const action = t.action}
				<button
					type="button"
					onclick={() => {
						dismiss(t.id);
						action.run();
					}}
					class="-my-0.5 shrink-0 rounded px-1.5 py-0.5 font-medium text-fg underline decoration-line-heavy underline-offset-4 transition-colors hover:decoration-fg"
				>
					{action.label}
				</button>
			{/if}
			<button
				type="button"
				onclick={() => dismiss(t.id)}
				class="text-fg-faint transition-colors hover:text-fg"
				aria-label="Dismiss"
			>
				<X class="size-3.5" />
			</button>
		</div>
	{/each}
</div>
