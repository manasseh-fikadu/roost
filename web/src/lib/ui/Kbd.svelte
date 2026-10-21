<!-- A key on the keyboard, such as ⌘ or K. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		children: Snippet;
	}

	let { children }: Props = $props();
</script>

<kbd
	class="inline-flex h-5 min-w-5 items-center justify-center rounded border border-line-strong bg-raised px-1 font-mono text-[11px] text-fg-subtle"
	>{@render children()}</kbd
>
