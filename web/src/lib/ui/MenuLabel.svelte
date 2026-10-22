<!-- Text in a <Menu> that isn't an item, such as who's signed in. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		children: Snippet;
	}

	let { children }: Props = $props();
</script>

<div class="px-2 py-1.5 text-[12px] text-fg-subtle">{@render children()}</div>
