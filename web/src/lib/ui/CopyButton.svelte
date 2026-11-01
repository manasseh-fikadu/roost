<!-- A button that copies a value to the clipboard, and says so. -->
<script lang="ts">
	import Check from '@lucide/svelte/icons/check';
	import Copy from '@lucide/svelte/icons/copy';
	import { copyText } from '#lib/clipboard.js';
	import { toast } from '#lib/toast.svelte.js';
	import IconButton from './IconButton.svelte';

	interface Props {
		value: string;
		/** What's copied, as in "Copy command". */
		label?: string;
	}

	let { value, label = 'Copy' }: Props = $props();

	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	async function copy() {
		try {
			await copyText(value);
		} catch {
			toast("Couldn't copy. Select the text and copy it yourself.", 'error');
			return;
		}
		copied = true;
		clearTimeout(timer);
		timer = setTimeout(() => (copied = false), 1500);
	}
</script>

<IconButton label={copied ? 'Copied' : label} size="sm" onclick={copy}>
	{#if copied}
		<Check class="size-3.5 text-green-fg" />
	{:else}
		<Copy class="size-3.5" />
	{/if}
</IconButton>
