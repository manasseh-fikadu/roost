<!-- A shell command on one line, with a button to copy it. -->
<script lang="ts">
	import CopyButton from './CopyButton.svelte';

	interface Props {
		command: string;
	}

	let { command }: Props = $props();
</script>

<div class="flex items-center gap-2 rounded-lg border border-line bg-canvas py-1.5 pr-1.5 pl-3">
	<span class="font-mono text-[12px] text-fg-faint select-none" aria-hidden="true">$</span>
	<code class="min-w-0 flex-1 overflow-x-auto font-mono text-[12.5px] whitespace-nowrap text-fg"
		>{command}</code
	>
	<CopyButton value={command} label="Copy command" />
</div>
