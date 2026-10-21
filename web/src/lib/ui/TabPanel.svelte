<!-- What one tab of <Tabs> shows. -->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Tabs } from 'bits-ui';

	interface Props {
		value: string;
		children: Snippet;
		class?: string;
	}

	let { value, children, class: className = 'pt-4' }: Props = $props();
</script>

<Tabs.Content {value} class={['outline-none', className]}>{@render children()}</Tabs.Content>
