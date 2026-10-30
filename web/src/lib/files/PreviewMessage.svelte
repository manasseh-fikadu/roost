<!-- A centered note in a preview: that it's loading, can't be shown, or failed. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import type { Icon } from '#lib/icon.js';

	interface Props {
		icon: Icon;
		title: string;
		tone?: 'default' | 'error';
		children?: Snippet;
	}

	let { icon: IconComponent, title, tone = 'default', children }: Props = $props();
</script>

<div class="flex size-full items-center justify-center p-6">
	<div class="max-w-sm text-center">
		<div
			class={[
				'mx-auto mb-3 flex size-11 items-center justify-center rounded-full border',
				tone === 'error'
					? 'border-red/35 bg-red/10 text-red-fg'
					: 'border-line-strong bg-raised text-fg-subtle'
			]}
		>
			<IconComponent class="size-4" />
		</div>
		<p class="text-[14px] font-medium text-fg">{title}</p>
		{#if children}
			<div class="mt-1.5 text-[13px] text-fg-subtle">{@render children()}</div>
		{/if}
	</div>
</div>
