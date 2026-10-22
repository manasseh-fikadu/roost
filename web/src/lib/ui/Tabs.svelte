<!--
	Tabs along a line, with a <TabPanel> for each inside. Arrow keys move
	between tabs.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Tabs } from 'bits-ui';

	interface Tab {
		value: string;
		label: string;
		/** A number after the label, such as how many items the tab holds. */
		count?: number;
	}

	interface Props {
		value?: string;
		tabs: Tab[];
		/** The <TabPanel>s. */
		children: Snippet;
		class?: string;
	}

	let { value = $bindable(), tabs, children, class: className }: Props = $props();
</script>

<Tabs.Root bind:value class={className}>
	<Tabs.List class="flex gap-1 overflow-x-auto border-b border-line">
		{#each tabs as tab (tab.value)}
			<Tabs.Trigger
				value={tab.value}
				class="-mb-px flex h-10 shrink-0 items-center gap-2 border-b-2 border-transparent px-3 text-[14px] text-fg-subtle transition-colors hover:text-fg data-[state=active]:border-fg data-[state=active]:text-fg"
			>
				{tab.label}
				{#if tab.count !== undefined}
					<span class="rounded-full bg-hover px-1.5 num text-[11px] text-fg-muted">{tab.count}</span
					>
				{/if}
			</Tabs.Trigger>
		{/each}
	</Tabs.List>
	{@render children()}
</Tabs.Root>
